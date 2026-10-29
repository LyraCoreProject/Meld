package vmangos

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"Meld/internal/clientpatch"
	"Meld/internal/mpq"
	"Meld/internal/sandbox"
)

func TestLyraPatchKeepsRealmArchiveAndPendingSeparate(t *testing.T) {
	data := t.TempDir()
	oldRoot := pendingRoot
	cache := t.TempDir()
	pendingRoot = func() string { return cache }
	t.Cleanup(func() { pendingRoot = oldRoot; clientpatch.Configure(data, false) })
	name := `DBFilesClient\ItemDisplayInfo.dbc`
	realm := mpq.Write([]mpq.File{{Name: name, Data: []byte("realm")}})
	if err := os.WriteFile(filepath.Join(data, "patch-3.MPQ"), realm, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddPending(data, []mpq.File{{Name: name, Data: []byte("vmangos pending")}}); err != nil {
		t.Fatal(err)
	}
	clientpatch.Configure(data, true)
	if pending, err := PendingFiles(data); err != nil || len(pending) != 0 {
		t.Fatalf("other archive's queue: %v %v", pending, err)
	}
	if err := AddPending(data, []mpq.File{{Name: name, Data: []byte("meld")}}); err != nil {
		t.Fatal(err)
	}
	if b, err := readClientFile(data, name); err != nil || string(b) != "meld" {
		t.Fatalf("overlay: %s %v", b, err)
	}
	patch, err := InstallWithPending(data, nil)
	if err != nil || filepath.Base(patch) != "patch-4.MPQ" {
		t.Fatalf("install: %s %v", patch, err)
	}
	if b, err := os.ReadFile(filepath.Join(data, "patch-3.MPQ")); err != nil || !bytes.Equal(b, realm) {
		t.Fatal("realm archive changed")
	}
	set, err := mpq.OpenDataDir(data)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if b, err := set.ReadFile(name); err != nil || string(b) != "meld" {
		t.Fatalf("patch order: %s %v", b, err)
	}
	if b, err := readUnpatched(set, name); err != nil || string(b) != "realm" {
		t.Fatalf("base read excluded realm: %s %v", b, err)
	}
	copyDir, err := sandbox.Prepare(data, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realInfo, _ := os.Stat(patch)
	copyInfo, err := os.Stat(filepath.Join(copyDir, "patch-4.MPQ"))
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(realInfo, copyInfo) {
		t.Fatal("sandbox linked Meld's patch")
	}
	clientpatch.Configure(data, false)
	pending, err := PendingFiles(data)
	if err != nil || len(pending) != 1 || string(pending[0].Data) != "vmangos pending" {
		t.Fatalf("lost VMaNGOS queue: %v %v", pending, err)
	}
}
