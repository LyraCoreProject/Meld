package vmangos

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"Meld/internal/lyracore"
	"Meld/internal/stdb"
)

// The limits of one operator_apply_spawn_edits call (LyraCore's docs/operator-content-api.md).
const (
	lyraMaxEdits     = 256
	lyraMaxWaypoints = 4096
)

// lyraPlacement is the Module's CreatureSpawnPlacement.
type lyraPlacement struct {
	MapID        uint32  `json:"map_id"`
	X            float32 `json:"x"`
	Y            float32 `json:"y"`
	Z            float32 `json:"z"`
	Orientation  float32 `json:"orientation"`
	MovementType uint8   `json:"movement_type"`
	RespawnSecs  uint32  `json:"respawn_secs"`
}

// lyraObjectFields is the Module's GameObjectSpawnFields.
type lyraObjectFields struct {
	TemplateEntry uint32  `json:"template_entry"`
	MapID         uint32  `json:"map_id"`
	X             float32 `json:"x"`
	Y             float32 `json:"y"`
	Z             float32 `json:"z"`
	Orientation   float32 `json:"orientation"`
	State         uint8   `json:"state"`
	Rotation0     float32 `json:"rotation_0"`
	Rotation1     float32 `json:"rotation_1"`
	Rotation2     float32 `json:"rotation_2"`
	Rotation3     float32 `json:"rotation_3"`
}

// lyraPoint is the Module's WaypointPoint.
type lyraPoint struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	Z float32 `json:"z"`
}

// lyraEdit is one SpawnEdit of the Module: an object with the variant's name as its one key.
type lyraEdit map[string]any

// ApplyLyraSpawnEdits writes a region's spawn edits into a LyraCore realm through its
// operator_apply_spawn_edits reducer, in one transaction: the realm shows them at once. The
// returned Result's SQL is the reducer's argument, kept as the record of the call.
func ApplyLyraSpawnEdits(realm *lyracore.Realm, dataDir, mapName string, x, y, size int, edits []SpawnEdit) (Result, error) {
	if err := checkRegion(x, y, size); err != nil {
		return Result{}, err
	}
	mapID, err := clientMapID(dataDir, mapName)
	if err != nil {
		return Result{}, err
	}
	list := make([]Edit, 0, len(edits))
	objects := map[uint64]bool{}
	for _, e := range edits {
		ed := Edit{Spawn: serverSpawn(e.Spawn, mapID, x, y), Created: e.Created, Deleted: e.Deleted}
		if e.Original != nil {
			o := serverSpawn(*e.Original, mapID, x, y)
			ed.Original = &o
		}
		if ed.Spawn.Kind == GameObject && !ed.Created && !ed.Deleted {
			objects[lyracore.ObjectGUID(ed.Spawn.GUID)] = true
		}
		list = append(list, ed)
	}
	// An update writes every field of the object, so the ones Meld doesn't edit keep the realm's.
	states := map[int]uint8{}
	err = lyraLookup(realm.DB, realm.Database, "game_gameobject", "guid", objects, func(r *stdb.Row) {
		states[lyracore.LowGUID(r.Uint("guid"))] = uint8(r.Uint("state"))
	})
	if err != nil {
		return Result{}, err
	}
	ops, res, err := lyraEdits(list, states)
	if err != nil || len(ops) == 0 {
		return res, err
	}
	b, err := json.Marshal(ops)
	if err != nil {
		return res, err
	}
	res.SQL = string(b)
	return res, realm.DB.Call(realm.Database, "operator_apply_spawn_edits", ops)
}

// lyraEdits turns spawn edits, in the server's coordinates, into the Module's SpawnEdits. states
// are the realm's state of the changed game objects, by guid. It refuses a list over one call's
// limits rather than split it, since one call is one transaction.
func lyraEdits(edits []Edit, states map[int]uint8) ([]lyraEdit, Result, error) {
	sorted := append([]Edit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Spawn.GUID < sorted[j].Spawn.GUID })
	var ops []lyraEdit
	var res Result
	res.Live = true
	waypoints := 0
	for _, e := range sorted {
		s := e.Spawn
		name := label(s)
		if s.Map > MaxGameMap || (!e.Created && s.GUID <= 0) {
			res.Skipped = append(res.Skipped, name+": not on a map the realm has")
			continue
		}
		if e.Deleted {
			if e.Created {
				continue
			}
			if s.Kind == Creature {
				ops = append(ops, lyraEdit{"deleteCreature": lyracore.CreatureGUID(s.GUID)})
			} else {
				ops = append(ops, lyraEdit{"deleteGameObject": lyracore.ObjectGUID(s.GUID)})
			}
			res.Deleted++
			continue
		}
		res.Skipped = append(res.Skipped, lyraUnsupported(s, e.Original)...)
		if s.Kind == GameObject {
			fields := lyraObject(s, 1)
			if e.Created {
				ops = append(ops, lyraEdit{"createGameObject": fields})
				res.Added++
			} else {
				fields.State = states[s.GUID]
				ops = append(ops, lyraEdit{"updateGameObject": map[string]any{"guid": lyracore.ObjectGUID(s.GUID), "fields": fields}})
				res.Changed++
			}
			continue
		}
		// A creature guid holds its entry, so another creature in its place is a new spawn.
		recreate := !e.Created && e.Original != nil && e.Original.Entry != s.Entry
		create := e.Created || recreate
		if recreate {
			ops = append(ops, lyraEdit{"deleteCreature": lyracore.CreatureGUID(s.GUID)})
		}
		place := lyraCreature(s)
		if create {
			ops = append(ops, lyraEdit{"createCreature": map[string]any{"entry": uint32(s.Entry), "placement": place}})
		} else {
			ops = append(ops, lyraEdit{"updateCreature": map[string]any{"guid": lyracore.CreatureGUID(s.GUID), "placement": place}})
		}
		if e.Created {
			res.Added++
		} else {
			res.Changed++
		}
		var was []PathPoint
		if e.Original != nil && !create {
			was = e.Original.Path
		}
		if samePath(s.Path, was) {
			continue
		}
		// The reducers return no guid, so a new spawn's patrol waits for the next save.
		if create {
			res.Skipped = append(res.Skipped, name+": its patrol; save again once the realm has given it a guid")
			continue
		}
		points := make([]lyraPoint, len(s.Path))
		for i, p := range s.Path {
			points[i] = lyraPoint{X: float32(p.X), Y: float32(p.Y), Z: float32(p.Z)}
		}
		waypoints += len(points)
		ops = append(ops, lyraEdit{"setCreatureWaypoints": map[string]any{"guid": lyracore.CreatureGUID(s.GUID), "points": points}})
	}
	if len(ops) > lyraMaxEdits || waypoints > lyraMaxWaypoints {
		return nil, Result{}, fmt.Errorf("%d edits with %d waypoints is over the realm's %d edits and %d waypoints a save; save fewer spawns at a time",
			len(ops), waypoints, lyraMaxEdits, lyraMaxWaypoints)
	}
	return ops, res, nil
}

func lyraCreature(s Spawn) lyraPlacement {
	lo, _ := respawn(s)
	move := s.MovementType
	if move < 0 || move > 2 {
		move = 0
	}
	return lyraPlacement{MapID: uint32(s.Map), X: float32(s.X), Y: float32(s.Y), Z: float32(s.Z), Orientation: float32(s.O),
		MovementType: uint8(move), RespawnSecs: uint32(lo)}
}

func lyraObject(s Spawn, state uint8) lyraObjectFields {
	q := quaternion(s)
	return lyraObjectFields{TemplateEntry: uint32(s.Entry), MapID: uint32(s.Map), X: float32(s.X), Y: float32(s.Y), Z: float32(s.Z),
		Orientation: float32(s.O), State: state, Rotation0: float32(q[0]), Rotation1: float32(q[1]), Rotation2: float32(q[2]), Rotation3: float32(q[3])}
}

// lyraUnsupported names the changes a LyraCore spawn has no field for, which the save leaves out.
func lyraUnsupported(s Spawn, orig *Spawn) []string {
	if orig == nil {
		return nil
	}
	var out []string
	note := func(what string) {
		out = append(out, fmt.Sprintf("%s: its %s; a LyraCore spawn has none", label(s), what))
	}
	if s.Entry == orig.Entry && math.Abs(s.Scale-orig.Scale) > 1e-3 {
		note("scale")
	}
	if s.Kind != Creature {
		return out
	}
	if s.Display != orig.Display || s.Equipment != orig.Equipment || s.StandState != orig.StandState || s.EmoteState != orig.EmoteState || s.SheathState != orig.SheathState {
		note("look (display, equipment or pose)")
	}
	if math.Abs(s.WanderDistance-orig.WanderDistance) > 1e-3 {
		note("wander distance")
	}
	if s.Leader != orig.Leader || s.FollowDist != orig.FollowDist || s.FollowAngle != orig.FollowAngle {
		note("formation")
	}
	if s.Pool != orig.Pool {
		note("pool")
	}
	return out
}

// A realm's templates, read once per realm and client, under templatesMu.
var (
	lyraCreatures = map[string][]CreatureTemplate{}
	lyraObjects   = map[string][]ObjectTemplate{}
)

// ListLyraCreatureTemplates lists every creature template of a LyraCore realm, dressed in its
// display, as ListCreatureTemplates does a world database's. LyraCore has no equipment templates.
func ListLyraCreatureTemplates(realm *lyracore.Realm, dataDir string) ([]CreatureTemplate, error) {
	key := realm.DB.URL + "|" + realm.Database + "|" + dataDir
	templatesMu.Lock()
	defer templatesMu.Unlock()
	if c, ok := lyraCreatures[key]; ok {
		return c, nil
	}
	tables, err := cachedModelTables(dataDir)
	if err != nil {
		return nil, err
	}
	tab, err := realm.DB.Query(realm.Database, "SELECT * FROM game_creature_template")
	if err != nil {
		return nil, err
	}
	var out []CreatureTemplate
	err = tab.Each(func(r *stdb.Row) error {
		c := CreatureTemplate{
			Entry: int(r.Uint("entry")), Name: r.String("name"), Subname: r.String("subname"),
			LevelMin: int(r.Uint("level")), Faction: int(r.Uint("faction_template")), NPCFlags: int(r.Uint("npc_flags")),
			Rank: int(r.Uint("rank")), Type: int(r.Uint("creature_type")), Display: int(r.Uint("display_id")), Scale: float32(r.Float("scale")),
		}
		c.LevelMax = max(int(r.Uint("max_level")), c.LevelMin)
		if c.Scale <= 0 {
			c.Scale = 1
		}
		look := tables.creatureLook(uint32(c.Display), gear{sheath: sheathMelee})
		c.Model, c.Look, c.DisplayScale = look.model, look.look.Query(), look.scale
		out = append(out, c)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Entry < out[j].Entry
	})
	lyraCreatures[key] = out
	return out, nil
}

// ListLyraObjectTemplates lists every game object template of a LyraCore realm with its model.
func ListLyraObjectTemplates(realm *lyracore.Realm, dataDir string) ([]ObjectTemplate, error) {
	key := realm.DB.URL + "|" + realm.Database + "|" + dataDir
	templatesMu.Lock()
	defer templatesMu.Unlock()
	if o, ok := lyraObjects[key]; ok {
		return o, nil
	}
	tables, err := cachedModelTables(dataDir)
	if err != nil {
		return nil, err
	}
	tab, err := realm.DB.Query(realm.Database, "SELECT * FROM game_gameobject_template")
	if err != nil {
		return nil, err
	}
	var out []ObjectTemplate
	err = tab.Each(func(r *stdb.Row) error {
		o := ObjectTemplate{Entry: int(r.Uint("entry")), Name: r.String("name"), Type: int(r.Uint("type_id")),
			Display: int(r.Uint("display_id")), Size: float32(r.Float("size"))}
		if o.Size <= 0 {
			o.Size = 1
		}
		o.Model = tables.objectModel(uint32(o.Display))
		out = append(out, o)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Entry < out[j].Entry
	})
	lyraObjects[key] = out
	return out, nil
}

// SearchLyraCreatures lists up to limit creature templates of a LyraCore realm whose name has text
// in it (or whose entry it is), as SearchCreatures does a world database's.
func SearchLyraCreatures(realm *lyracore.Realm, dataDir, text string, limit int) ([]CreatureTemplate, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	all, err := ListLyraCreatureTemplates(realm, dataDir)
	if err != nil {
		return nil, err
	}
	entry, err := strconv.Atoi(text)
	isEntry := err == nil
	lower := strings.ToLower(text)
	var out []CreatureTemplate
	for _, c := range all {
		if len(out) >= max(limit, 1) {
			break
		}
		if (isEntry && c.Entry == entry) || strings.Contains(strings.ToLower(c.Name), lower) {
			out = append(out, c)
		}
	}
	return out, nil
}
