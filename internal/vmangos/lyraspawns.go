package vmangos

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"Meld/internal/adt"
	"Meld/internal/lyracore"
	"Meld/internal/mpq"
	"Meld/internal/stdb"
)

// lyraBatch is how many keys one OR-chained lookup names: SpacetimeDB's SQL has no IN.
const lyraBatch = 150

// ReadLyraSpawns returns the creatures and game objects a LyraCore realm places in the block of
// size tiles at (x, y) of the named map: its game_creature_spawn and game_gameobject rows (the
// spawns themselves, instance 0, not the live copies an instance makes), dressed from their
// templates like ReadSpawns' and with the creatures' patrol paths (game_creature_waypoint). The map
// ID comes from the client's Map.dbc, the map's database from the realm's shard map.
//
// LyraCore keeps less than VMaNGOS: no equipment, addon pose, events, pools or formations, and a
// creature's respawn time is one number. A spawn's guid is the low 48 bits of LyraCore's (see
// lyracore.LowGUID).
func ReadLyraSpawns(realm *lyracore.Realm, dataDir, mapName string, x, y, size int) ([]WorldSpawn, error) {
	if err := checkRegion(x, y, size); err != nil {
		return nil, err
	}
	mapID, err := clientMapID(dataDir, mapName)
	if err != nil {
		return nil, err
	}
	db := realm.Database(uint32(mapID))
	t := float64(adt.TileSize)
	zp := float64(adt.ZeroPoint)
	xLo, xHi := zp-float64(y+size)*t, zp-float64(y)*t
	yLo, yHi := zp-float64(x+size)*t, zp-float64(x)*t
	bounds := fmt.Sprintf("map_id = %d AND instance_id = 0 AND x >= %f AND x <= %f AND y >= %f AND y <= %f", mapID, xLo, xHi, yLo, yHi)

	// A row with a source_guid is the live copy a population spawn makes of itself with the entry it
	// drew (pass_respawn in LyraCore's lifecycle.rs), not a spawn of its own.
	creatures, err := realm.DB.Query(db, "SELECT * FROM game_creature_spawn WHERE "+bounds+" AND source_guid = 0")
	if err != nil {
		return nil, err
	}
	objects, err := realm.DB.Query(db, "SELECT * FROM game_gameobject WHERE "+bounds)
	if err != nil {
		return nil, err
	}

	var rows []spawnRow
	var fullGUIDs []uint64
	entries := map[uint64]bool{}
	err = creatures.Each(func(r *stdb.Row) error {
		var s spawnRow
		guid := r.Uint("guid")
		s.GUID = lyracore.LowGUID(guid)
		s.Entry = int(r.Uint("entry"))
		s.sx, s.sy, s.height = float32(r.Float("x")), float32(r.Float("y")), float32(r.Float("z"))
		s.Orientation = float32(r.Float("orientation"))
		s.MovementType = int(r.Uint("movement_type"))
		s.RespawnSecs = int(r.Uint("respawn_secs"))
		s.RespawnMaxSecs = s.RespawnSecs
		s.Kind = KindMonster
		rows = append(rows, s)
		fullGUIDs = append(fullGUIDs, guid)
		entries[uint64(s.Entry)] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	creatureCount := len(rows)
	objectEntries := map[uint64]bool{}
	err = objects.Each(func(r *stdb.Row) error {
		var s spawnRow
		s.GUID = lyracore.LowGUID(r.Uint("guid"))
		s.Entry = int(r.Uint("template_entry"))
		s.sx, s.sy, s.height = float32(r.Float("x")), float32(r.Float("y")), float32(r.Float("z"))
		s.Orientation = float32(r.Float("orientation"))
		s.Kind = KindObject
		q := [4]float64{r.Float("rotation_0"), r.Float("rotation_1"), r.Float("rotation_2"), r.Float("rotation_3")}
		s.Tilt = tiltOf(float64(s.Orientation), q)
		rows = append(rows, s)
		objectEntries[uint64(s.Entry)] = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = lyraLookup(realm.DB, db, "game_creature_template", "entry", entries, func(r *stdb.Row) {
		entry := int(r.Uint("entry"))
		for i := range rows[:creatureCount] {
			s := &rows[i]
			if s.Entry != entry {
				continue
			}
			s.Name, s.Subname = r.String("name"), r.String("subname")
			s.LevelMin = int(r.Uint("level"))
			s.LevelMax = max(int(r.Uint("max_level")), s.LevelMin)
			s.Faction = int(r.Uint("faction_template"))
			s.NPCFlags = int(r.Uint("npc_flags"))
			s.Rank = int(r.Uint("rank"))
			s.Type = int(r.Uint("creature_type"))
			s.display = uint32(r.Uint("display_id"))
			s.scale = float32(r.Float("scale"))
			if s.NPCFlags != 0 {
				s.Kind = KindNPC
			}
		}
	})
	if err != nil {
		return nil, err
	}
	err = lyraLookup(realm.DB, db, "game_gameobject_template", "entry", objectEntries, func(r *stdb.Row) {
		entry := int(r.Uint("entry"))
		for i := range rows[creatureCount:] {
			s := &rows[creatureCount+i]
			if s.Entry != entry {
				continue
			}
			s.Name = r.String("name")
			s.Type = int(r.Uint("type_id"))
			s.display = uint32(r.Uint("display_id"))
			s.scale = float32(r.Float("size"))
			s.RespawnSecs = int(r.Uint("respawn_secs"))
			s.RespawnMaxSecs = s.RespawnSecs
		}
	})
	if err != nil {
		return nil, err
	}

	tables, err := cachedModelTables(dataDir)
	if err != nil {
		return nil, err
	}
	out := make([]WorldSpawn, len(rows))
	for i, s := range rows {
		out[i] = regionSpawn(s, x, y, tables, [3]heldItem{})
		if s.Kind != KindObject {
			// LyraCore has no sheath state: a creature's weapons, when it has any, stay put away.
			out[i].SheathState = 1
		}
	}
	if err := readLyraWaypoints(realm.DB, db, out[:creatureCount], fullGUIDs, x, y); err != nil {
		return nil, err
	}
	return out, nil
}

// readLyraWaypoints gives the creatures their patrol paths: a creature with game_creature_waypoint
// rows walks them in id order (LyraCore's patrol, whatever its movement type says), so it shows as
// one that walks waypoints (movement type 2).
func readLyraWaypoints(c *stdb.Client, db string, spawns []WorldSpawn, guids []uint64, x, y int) error {
	keys := map[uint64]bool{}
	for _, g := range guids {
		keys[g] = true
	}
	byGUID := map[uint64]int{}
	for i, g := range guids {
		byGUID[g] = i
	}
	type point struct {
		id uint64
		w  Waypoint
	}
	paths := map[int][]point{}
	t := float64(adt.TileSize)
	zp := float64(adt.ZeroPoint)
	err := lyraLookup(c, db, "game_creature_waypoint", "creature_guid", keys, func(r *stdb.Row) {
		i, ok := byGUID[r.Uint("creature_guid")]
		if !ok {
			return
		}
		sx, sy := r.Float("x"), r.Float("y")
		paths[i] = append(paths[i], point{id: r.Uint("id"), w: Waypoint{
			X: float32(zp - sy - float64(x)*t),
			Z: float32(zp - sx - float64(y)*t),
			Y: float32(r.Float("z")),
		}})
	})
	if err != nil {
		return err
	}
	for i, p := range paths {
		sort.Slice(p, func(a, b int) bool { return p[a].id < p[b].id })
		w := make([]Waypoint, len(p))
		for k := range p {
			w[k] = p[k].w
		}
		spawns[i].Waypoints = w
		spawns[i].MovementType = 2
	}
	return nil
}

// lyraLookup reads the table's rows whose column holds one of the keys, in batches of OR-chained
// equalities, and hands each to f.
func lyraLookup(c *stdb.Client, db, table, column string, keys map[uint64]bool, f func(r *stdb.Row)) error {
	list := make([]uint64, 0, len(keys))
	for k := range keys {
		list = append(list, k)
	}
	sort.Slice(list, func(a, b int) bool { return list[a] < list[b] })
	for len(list) > 0 {
		n := min(len(list), lyraBatch)
		var where strings.Builder
		for i, k := range list[:n] {
			if i > 0 {
				where.WriteString(" OR ")
			}
			where.WriteString(column + " = " + strconv.FormatUint(k, 10))
		}
		list = list[n:]
		tab, err := c.Query(db, "SELECT * FROM "+table+" WHERE "+where.String())
		if err != nil {
			return err
		}
		if err := tab.Each(func(r *stdb.Row) error { f(r); return nil }); err != nil {
			return err
		}
	}
	return nil
}

// clientMapID finds the map ID of a map directory in the client's Map.dbc (the patches' newest).
func clientMapID(dataDir, mapName string) (int, error) {
	set, err := mpq.OpenDataDir(dataDir)
	if err != nil {
		return 0, err
	}
	defer set.Close()
	dbc, err := set.ReadFile(`DBFilesClient\Map.dbc`)
	if err != nil {
		return 0, err
	}
	for _, id := range MapRowIDs(dbc) {
		if strings.EqualFold(MapRowDir(dbc, id), mapName) {
			return int(id), nil
		}
	}
	return 0, errors.New("the client has no map " + mapName)
}
