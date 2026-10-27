package vmangos

import (
	"os"
	"path/filepath"
	"testing"

	"Meld/internal/lyracore"
)

// TestReadLyraSpawnsLive reads Northshire's spawns from a running LyraCore realm. It runs only with
// LYRA_URL (the SpacetimeDB server), LYRA_TOKEN (an owner token, or LYRA_TOKEN_FILE) and WOW_DIR set.
func TestReadLyraSpawnsLive(t *testing.T) {
	url, wow := os.Getenv("LYRA_URL"), os.Getenv("WOW_DIR")
	if url == "" || wow == "" {
		t.Skip("LYRA_URL and WOW_DIR not set")
	}
	token := os.Getenv("LYRA_TOKEN")
	if f := os.Getenv("LYRA_TOKEN_FILE"); token == "" && f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		token = string(b)
	}
	realm, err := lyracore.Open(lyracore.Settings{URL: url, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	spawns, err := ReadLyraSpawns(realm, filepath.Join(wow, "Data"), "Azeroth", 32, 48, 1)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	named, modelled, paths := 0, 0, 0
	for _, s := range spawns {
		kinds[s.Kind]++
		if s.Name != "" {
			named++
		}
		if s.Model != "" {
			modelled++
		}
		if len(s.Waypoints) > 0 {
			paths++
		}
		if s.X < 0 || s.X > 533.4 || s.Z < 0 || s.Z > 533.4 {
			t.Errorf("%s %d at (%v, %v) is outside the tile", s.Name, s.GUID, s.X, s.Z)
		}
	}
	t.Logf("%d spawns %v, %d named, %d with a model, %d with a path", len(spawns), kinds, named, modelled, paths)
	if kinds[KindMonster] == 0 || kinds[KindNPC] == 0 || kinds[KindObject] == 0 {
		t.Fatalf("Northshire lacks a kind: %v", kinds)
	}
	if named < len(spawns)*9/10 || modelled < len(spawns)*9/10 {
		t.Fatalf("only %d named and %d modelled of %d", named, modelled, len(spawns))
	}
}
