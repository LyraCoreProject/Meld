package lyracore

import "testing"

func TestOpen(t *testing.T) {
	r, err := Open(Settings{URL: " http://x "})
	if err != nil {
		t.Fatal(err)
	}
	if r.Database != DefaultDatabase || r.DB.URL != "http://x" {
		t.Errorf("opened %q at %q", r.Database, r.DB.URL)
	}
	if _, err := Open(Settings{}); err == nil {
		t.Error("no URL opened")
	}
}

func TestGUIDs(t *testing.T) {
	const creature = uint64(17379390974119196744)
	low := LowGUID(creature)
	if CreatureGUID(low) != creature {
		t.Fatalf("creature guid %x came back as %x", creature, CreatureGUID(low))
	}
	if low >= 1<<53 {
		t.Fatalf("low guid %d is no exact JavaScript number", low)
	}
	if got := PackCreatureGUID(721, SpawnID(low)); got != low {
		t.Fatalf("packed %x, want %x", got, low)
	}
	const object = uint64(17370383762768029812)
	if ObjectGUID(LowGUID(object)) != object {
		t.Fatal("object guid did not come back")
	}
}
