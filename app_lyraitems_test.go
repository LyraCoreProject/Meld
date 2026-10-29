package main

import (
	"os"
	"path/filepath"
	"testing"

	"Meld/internal/clientpatch"
	"Meld/internal/mpq"
	"Meld/internal/vmangos"
)

func TestLyraClientPatchChoice(t *testing.T) {
	for _, mode := range []string{"vmangos", "session", "config"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			data := filepath.Join(root, "Data")
			if err := os.Mkdir(data, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(data, "terrain.MPQ"), mpq.Write([]mpq.File{{Name: "base", Data: []byte("base")}}), 0o644); err != nil {
				t.Fatal(err)
			}
			a := &App{gameUp: func() bool { return false }}
			a.cfg.WowPath = root
			want := "patch-3.MPQ"
			if mode == "session" {
				a.lyraSession = &lyraSession{url: "http://invalid", tokenFile: "missing"}
				want = "patch-4.MPQ"
			}
			if mode == "config" {
				a.cfg.LyraCore.URL = "http://invalid"
				want = "patch-4.MPQ"
			}
			dir, err := a.clientDataDir()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { clientpatch.Configure(data, false) })
			if clientpatch.Name(dir) != want {
				t.Fatalf("patch: %s", clientpatch.Name(dir))
			}
			path, err := a.installClientFiles(dir, []mpq.File{{Name: "test", Data: []byte("meld")}})
			if err != nil || filepath.Base(path) != want {
				t.Fatalf("write: %s %v", path, err)
			}
			if mode != "vmangos" {
				if _, err := os.Stat(filepath.Join(data, vmangos.PatchName)); !os.IsNotExist(err) {
					t.Fatal("wrote the realm's patch-3")
				}
			}
		})
	}
}

func TestLyraItemSQLAndSandboxWritesRefused(t *testing.T) {
	a := &App{lyraSession: &lyraSession{url: "http://invalid"}, sandboxData: "sandbox"}
	if _, err := a.ExportItems([]int{25}, "items"); err == nil {
		t.Fatal("SQL export allowed")
	}
	if _, err := a.PlanItemImport("not-read"); err == nil {
		t.Fatal("SQL import plan allowed")
	}
	if _, err := a.ImportItems("not-read"); err == nil {
		t.Fatal("SQL import allowed")
	}
	if _, err := a.SaveItem(25, 0, map[string]string{"name": "Changed"}); err != errSandboxed {
		t.Fatalf("sandbox item: %v", err)
	}
	if _, err := a.CreateItem(0, "New"); err != errSandboxed {
		t.Fatalf("sandbox create: %v", err)
	}
	if _, err := a.SaveVendor(620, nil); err != errSandboxed {
		t.Fatalf("sandbox vendor: %v", err)
	}
}
