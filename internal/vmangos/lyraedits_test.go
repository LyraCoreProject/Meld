package vmangos

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Meld/internal/lyracore"
)

// lyraJSON is lyraEdits' output as the reducer's argument.
func lyraJSON(t *testing.T, edits []Edit, states map[int]uint8) (string, Result) {
	t.Helper()
	ops, res, err := lyraEdits(edits, states)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(ops)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), res
}

func TestLyraEditsAddChangeDelete(t *testing.T) {
	wolf := lyracore.PackCreatureGUID(299, 7)
	moved := Spawn{Kind: Creature, GUID: wolf, Entry: 299, Map: 0, X: 1, Y: 2, Z: 3, O: 0.5, RespawnMin: 120, MovementType: 1}
	orig := moved
	orig.X = 0
	edits := []Edit{
		{Spawn: moved, Original: &orig},
		{Spawn: Spawn{Kind: Creature, GUID: 1, Entry: 620, Map: 0, X: 4, Y: 5, Z: 6}, Created: true},
		{Spawn: Spawn{Kind: GameObject, GUID: 9, Entry: 1731, Map: 0}, Deleted: true},
		{Spawn: Spawn{Kind: GameObject, GUID: 10, Entry: 1731, Map: 0}, Created: true, Deleted: true},
		{Spawn: Spawn{Kind: GameObject, GUID: 11, Entry: 1732, Map: 0, O: 1}, Original: &Spawn{Kind: GameObject, GUID: 11, Entry: 1731}},
	}
	got, res := lyraJSON(t, edits, map[int]uint8{11: 0})
	if res.Added != 1 || res.Changed != 2 || res.Deleted != 1 || !res.Live || len(res.Skipped) != 0 {
		t.Fatalf("result %+v", res)
	}
	for _, want := range []string{
		`{"createCreature":{"entry":620,"placement":{"map_id":0,"x":4,"y":5,"z":6,"orientation":0,"movement_type":0,"respawn_secs":300}}}`,
		`{"deleteGameObject":17370383762768003081}`,
		`{"updateGameObject":{"fields":{"template_entry":1732,"map_id":0,"x":0,"y":0,"z":0,"orientation":1,"state":0,`,
		`{"updateCreature":{"guid":` + jsonUint(lyracore.CreatureGUID(wolf)) + `,"placement":{"map_id":0,"x":1,"y":2,"z":3,"orientation":0.5,"movement_type":1,"respawn_secs":120}}}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, "17370383762768003082") {
		t.Errorf("a spawn added and deleted again was sent: %s", got)
	}
}

func TestLyraEditsRecreateAnotherCreature(t *testing.T) {
	g := lyracore.PackCreatureGUID(299, 7)
	s := Spawn{Kind: Creature, GUID: g, Entry: 300, Path: []PathPoint{{X: 1}}}
	orig := Spawn{Kind: Creature, GUID: g, Entry: 299}
	got, res := lyraJSON(t, []Edit{{Spawn: s, Original: &orig}}, nil)
	del := strings.Index(got, `"deleteCreature":`+jsonUint(lyracore.CreatureGUID(g)))
	add := strings.Index(got, `"createCreature":{"entry":300`)
	if del < 0 || add < del || strings.Contains(got, "setCreatureWaypoints") {
		t.Fatalf("not a delete then a create without a patrol: %s", got)
	}
	if res.Changed != 1 || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "patrol") {
		t.Fatalf("result %+v", res)
	}
}

func TestLyraEditsPatrol(t *testing.T) {
	g := lyracore.PackCreatureGUID(299, 7)
	orig := Spawn{Kind: Creature, GUID: g, Entry: 299, MovementType: 2, Path: []PathPoint{{X: 1}}}
	s := orig
	s.Path = []PathPoint{{X: 1}, {X: 2, Y: 3, Z: 4}}
	got, _ := lyraJSON(t, []Edit{{Spawn: s, Original: &orig}}, nil)
	want := `{"setCreatureWaypoints":{"guid":` + jsonUint(lyracore.CreatureGUID(g)) + `,"points":[{"x":1,"y":0,"z":0},{"x":2,"y":3,"z":4}]}}`
	if !strings.Contains(got, want) {
		t.Fatalf("missing %s in %s", want, got)
	}
	got, _ = lyraJSON(t, []Edit{{Spawn: orig, Original: &orig}}, nil)
	if strings.Contains(got, "setCreatureWaypoints") {
		t.Fatalf("an unchanged patrol was sent: %s", got)
	}
}

func TestLyraEditsReportUnsupportedFields(t *testing.T) {
	orig := Spawn{Kind: Creature, GUID: 5, Entry: 299, Scale: 1}
	s := orig
	s.Scale, s.Equipment, s.Pool = 2, 4, 3
	_, res := lyraJSON(t, []Edit{{Spawn: s, Original: &orig}}, nil)
	if len(res.Skipped) != 3 {
		t.Fatalf("skipped %q", res.Skipped)
	}
}

func TestLyraEditsRefuseOverOneCall(t *testing.T) {
	var edits []Edit
	for i := range lyraMaxEdits + 1 {
		edits = append(edits, Edit{Spawn: Spawn{Kind: GameObject, GUID: i + 1, Entry: 1}, Deleted: true})
	}
	if _, _, err := lyraEdits(edits, nil); err == nil {
		t.Fatal("a save over the realm's limit was split or sent")
	}
}

func jsonUint(v uint64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestApplyLyraSpawnEditsLive moves a Northshire creature a yard through the realm's operator
// reducer, reads it back at its new place and moves it back. It runs only with LYRA_URL,
// LYRA_TOKEN_FILE (the Operator's token) and WOW_DIR set, against a realm whose spawns may change.
func TestApplyLyraSpawnEditsLive(t *testing.T) {
	url, file, wow := os.Getenv("LYRA_URL"), os.Getenv("LYRA_TOKEN_FILE"), os.Getenv("WOW_DIR")
	if url == "" || file == "" || wow == "" {
		t.Skip("LYRA_URL, LYRA_TOKEN_FILE and WOW_DIR not set")
	}
	token, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	realm, err := lyracore.Open(lyracore.Settings{URL: url, Token: string(token)})
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(wow, "Data")
	find := func(guid int) *WorldSpawn {
		spawns, err := ReadLyraSpawns(realm, data, "Azeroth", 32, 48, 1)
		if err != nil {
			t.Fatal(err)
		}
		for i := range spawns {
			if spawns[i].GUID == guid || (guid == 0 && spawns[i].Kind == KindMonster && len(spawns[i].Waypoints) == 0) {
				return &spawns[i]
			}
		}
		return nil
	}
	orig := find(0)
	if orig == nil {
		t.Fatal("Northshire has no idle monster")
	}
	moved := *orig
	moved.X++
	res, err := ApplyLyraSpawnEdits(realm, data, "Azeroth", 32, 48, 1, []SpawnEdit{{Spawn: moved, Original: orig}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		back := *orig
		if _, err := ApplyLyraSpawnEdits(realm, data, "Azeroth", 32, 48, 1, []SpawnEdit{{Spawn: back, Original: &moved}}); err != nil {
			t.Errorf("moving %s back: %v", orig.Name, err)
		}
	}()
	if res.Changed != 1 || !res.Live {
		t.Fatalf("result %+v", res)
	}
	now := find(orig.GUID)
	if now == nil || math.Abs(float64(now.X-moved.X)) > 0.01 {
		t.Fatalf("%s %d is at %+v after the move, want x %v", orig.Name, orig.GUID, now, moved.X)
	}
}
