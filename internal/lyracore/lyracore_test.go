package lyracore

import "testing"

func TestDatabaseFollowsShardMap(t *testing.T) {
	r, err := Open(Settings{URL: "http://x", Shards: `"36:*=lyracore-instances, 1:*=lyracore-world-1, 450:*=lyracore-world-1, 0:7=other"`})
	if err != nil {
		t.Fatal(err)
	}
	for m, want := range map[uint32]string{0: "lyracore", 1: "lyracore-world-1", 36: "lyracore-instances", 450: "lyracore-world-1", 13: "lyracore"} {
		if got := r.Database(m); got != want {
			t.Errorf("map %d: %s, want %s", m, got, want)
		}
	}
	if _, err := ParseShards("1:*"); err == nil {
		t.Error("an entry without '=' parsed")
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
