// Package lyracore is a LyraCore realm as Meld reaches it: the Native Host's SpacetimeDB and the one
// database (the realm's Shard) that holds every map's spawns, terrain and players.
package lyracore

import (
	"errors"
	"strings"

	"Meld/internal/stdb"
)

// Settings are the config's LyraCore fields (config.LyraCore).
type Settings struct {
	// URL is the SpacetimeDB server's base URL, such as http://127.0.0.1:3000 (through an SSH
	// tunnel when the server listens on the realm machine's loopback only).
	URL string
	// Token is the Operator's token: the operator reducers and SQL that writes need it.
	Token string
	// Database is the realm's Shard, "lyracore" when empty.
	Database string
}

// Realm is a LyraCore realm Meld reads and writes.
type Realm struct {
	DB *stdb.Client
	// Database is the realm's Shard.
	Database string
}

// DefaultDatabase is LyraCore's database name when none is set.
const DefaultDatabase = "lyracore"

// Open makes a Realm of the settings. It does not reach the server.
func Open(s Settings) (*Realm, error) {
	if strings.TrimSpace(s.URL) == "" {
		return nil, errors.New("no LyraCore SpacetimeDB URL is set (Preferences › LyraCore Realm…)")
	}
	db := strings.TrimSpace(s.Database)
	if db == "" {
		db = DefaultDatabase
	}
	return &Realm{DB: &stdb.Client{URL: strings.TrimSpace(s.URL), Token: strings.TrimSpace(s.Token)}, Database: db}, nil
}

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
