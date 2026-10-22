// Package lyracore is a LyraCore realm as Meld reaches it: the SpacetimeDB server its world lives
// in, and which of the realm's databases (shards) owns each map. LyraCore splits one realm over
// several databases, as its gateway's LYRACORE_SHARD_MAP says (a map's spawns, terrain and players
// live in that map's database), so every read and write must pick the map's one.
package lyracore

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"Meld/internal/stdb"
)

// Settings are the config's LyraCore fields (config.LyraCore).
type Settings struct {
	// URL is the SpacetimeDB server's base URL, such as http://127.0.0.1:3000 (through an SSH
	// tunnel when the server listens on the realm machine's loopback only).
	URL string
	// Token is a token of the databases' owner: SQL that writes (and the operator-only reducers)
	// needs it.
	Token string
	// Database is the database of every map the shard map leaves out (the gateway's
	// LYRACORE_DATABASE), "lyracore" when empty.
	Database string
	// Shards is the gateway's LYRACORE_SHARD_MAP: "map:instance=database" pairs split by commas,
	// such as "1:*=lyracore-world-1, 36:*=lyracore-instances".
	Shards string
}

// Realm is a LyraCore realm Meld reads and writes.
type Realm struct {
	DB       *stdb.Client
	database string
	shards   map[uint32]string
}

// DefaultDatabase is LyraCore's database name when none is set.
const DefaultDatabase = "lyracore"

// Open makes a Realm of the settings. It does not reach the server.
func Open(s Settings) (*Realm, error) {
	if strings.TrimSpace(s.URL) == "" {
		return nil, errors.New("no LyraCore SpacetimeDB URL is set (Preferences › LyraCore Realm…)")
	}
	shards, err := ParseShards(s.Shards)
	if err != nil {
		return nil, err
	}
	db := strings.TrimSpace(s.Database)
	if db == "" {
		db = DefaultDatabase
	}
	return &Realm{DB: &stdb.Client{URL: strings.TrimSpace(s.URL), Token: strings.TrimSpace(s.Token)}, database: db, shards: shards}, nil
}

// Database is the name of the database that owns a map.
func (r *Realm) Database(mapID uint32) string {
	if db, ok := r.shards[mapID]; ok {
		return db
	}
	return r.database
}

// ParseShards reads a LYRACORE_SHARD_MAP. Only whole-map entries ("map:*") are kept: Meld edits
// a map's spawns, not one instance's.
func ParseShards(s string) (map[uint32]string, error) {
	out := map[uint32]string{}
	s = strings.Trim(strings.TrimSpace(s), `"`)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, db, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("shard map entry %q has no '='", part)
		}
		m, inst, _ := strings.Cut(strings.TrimSpace(key), ":")
		id, err := strconv.ParseUint(strings.TrimSpace(m), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("shard map entry %q: map %q is not a number", part, m)
		}
		if inst = strings.TrimSpace(inst); inst != "" && inst != "*" {
			continue
		}
		out[uint32(id)] = strings.TrimSpace(db)
	}
	return out, nil
}

// The high parts of LyraCore's spawn guids (its ids.rs): a creature spawn's guid is
// 0xF130<<48 | entry<<24 | spawn id, a game object's 0xF110<<48 | spawn id.
const (
	HighUnit       = 0xF130
	HighGameObject = 0xF110
	// lowMask keeps a guid's low 48 bits: what Meld calls the spawn's guid, since the editor's
	// numbers are JavaScript ones and a whole LyraCore guid needs 64 bits.
	lowMask = 1<<48 - 1
	// ComponentMask bounds a creature guid's entry and spawn id (24 bits each).
	ComponentMask = 1<<24 - 1
)

// LowGUID is the part of a LyraCore guid Meld keeps.
func LowGUID(guid uint64) int {
	return int(guid & lowMask)
}

// CreatureGUID is a creature spawn's whole guid from Meld's.
func CreatureGUID(low int) uint64 {
	return HighUnit<<48 | uint64(low)&lowMask
}

// ObjectGUID is a game object spawn's whole guid from Meld's.
func ObjectGUID(low int) uint64 {
	return HighGameObject<<48 | uint64(low)&lowMask
}

// PackCreatureGUID packs a creature spawn's guid as LyraCore does, Meld's part of it.
func PackCreatureGUID(entry, spawnID uint32) int {
	return int(uint64(entry&ComponentMask)<<24 | uint64(spawnID&ComponentMask))
}

// SpawnID is the spawn id in Meld's part of a creature guid.
func SpawnID(low int) uint32 {
	return uint32(low) & ComponentMask
}
