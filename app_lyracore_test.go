package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLyraSessionFromEnvironment(t *testing.T) {
	t.Setenv(envLyraURL, "")
	if lyraSessionFromEnv() != nil {
		t.Fatal("a session with no URL")
	}
	file := filepath.Join(t.TempDir(), "native-owner-token")
	if err := os.WriteFile(file, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envLyraURL, "http://127.0.0.1:3000")
	t.Setenv(envLyraDatabase, "lyracore")
	t.Setenv(envLyraTokenFile, file)
	a := &App{lyraSession: lyraSessionFromEnv()}
	if info := a.LyraCore(); !info.Session || info.URL != "http://127.0.0.1:3000" || !info.HasToken {
		t.Fatalf("info %+v", info)
	}
	// A token `lyracore dev up` writes again is read at the next call.
	if err := os.WriteFile(file, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	realm, err := a.lyraRealm()
	if err != nil {
		t.Fatal(err)
	}
	if realm.DB.Token != "second" || realm.Database != "lyracore" {
		t.Fatalf("realm %q with token %q", realm.Database, realm.DB.Token)
	}
	if err := a.SetLyraCore("http://elsewhere", "", ""); err == nil {
		t.Fatal("the session's realm was replaced")
	}
}
