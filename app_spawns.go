package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"Meld/internal/vmangos"
)

// LoadSpawns reads the creatures and game objects the server's world database places in a block of
// tiles. It needs the VMaNGOS server folder, and the server's database running, or else a LyraCore
// realm, whose SpacetimeDB it reads instead when one is set.
func (a *App) LoadSpawns(mapName string, x, y, size int) ([]vmangos.WorldSpawn, error) {
	if realm, dataDir, err := a.lyraRealmAndData(); err != nil || realm != nil {
		if err != nil {
			return nil, err
		}
		return vmangos.ReadLyraSpawns(realm, dataDir, mapName, x, y, size)
	}
	a.cfgMu.Lock()
	server := a.cfg.VmangosPath
	a.cfgMu.Unlock()
	if server == "" {
		return nil, errors.New("set the VMaNGOS server folder first (Preferences › VMaNGOS Server Folder)")
	}
	dataDir, err := a.clientDataDir()
	if err != nil {
		return nil, err
	}
	return vmangos.ReadSpawns(server, dataDir, mapName, x, y, size)
}

// SpawnLook dresses a creature display with an equipment and a pose, for a spawn whose display,
// equipment, stand state, emote or sheath state was edited.
func (a *App) SpawnLook(display, equipment, stand, emote, sheath int) (vmangos.LookResult, error) {
	if realm, dataDir, err := a.lyraRealmAndData(); err != nil || realm != nil {
		if err != nil {
			return vmangos.LookResult{}, err
		}
		// Realm scales already include the client display scale.
		look, err := vmangos.CreatureLook("", dataDir, display, 0, stand, emote, sheath)
		look.DisplayScale = 1
		return look, err
	}
	server, dataDir, err := a.serverAndData()
	if err != nil {
		return vmangos.LookResult{}, err
	}
	return vmangos.CreatureLook(server, dataDir, display, equipment, stand, emote, sheath)
}

func (a *App) ListWeapons() ([]vmangos.WeaponEntry, error) {
	if a.LyraCore().URL != "" {
		return nil, errors.New("equipment templates are not available on a LyraCore realm")
	}
	server, dataDir, err := a.serverAndData()
	if err != nil {
		return nil, err
	}
	return vmangos.ListWeapons(server, dataDir)
}

// Items are ordered main hand, off hand, ranged.
func (a *App) EquipmentItems(id int) ([3]int, error) {
	if a.LyraCore().URL != "" {
		return [3]int{}, errors.New("equipment templates are not available on a LyraCore realm")
	}
	server, _, err := a.serverAndData()
	if err != nil {
		return [3]int{}, err
	}
	return vmangos.EquipmentItems(server, id)
}

// Display IDs are ordered main hand, off hand, ranged.
func (a *App) EquipmentDisplays(ids []int) (map[int][3]int, error) {
	if a.LyraCore().URL != "" {
		return nil, errors.New("equipment templates are not available on a LyraCore realm")
	}
	server, _, err := a.serverAndData()
	if err != nil {
		return nil, err
	}
	return vmangos.EquipmentDisplays(server, ids)
}

// Creates a missing template for main hand, off hand and ranged; all zero returns 0.
func (a *App) EquipmentFor(items [3]int) (int, error) {
	if a.LyraCore().URL != "" {
		return 0, errors.New("equipment templates are not available on a LyraCore realm")
	}
	server, _, err := a.serverForWrite()
	if err != nil {
		return 0, err
	}
	return vmangos.EquipmentFor(server, items)
}

// GameEvents lists the world database's events (holidays, the Darkmoon Faire, invasions), for showing
// the spawns of each.
func (a *App) GameEvents() ([]vmangos.GameEvent, error) {
	server, _, err := a.serverAndData()
	if err != nil {
		return nil, err
	}
	return vmangos.GameEvents(server)
}

// SpawnEmotes lists the client's emotes, for picking a spawn's looping emote.
func (a *App) SpawnEmotes() ([]vmangos.Emote, error) {
	dataDir, err := a.clientDataDir()
	if err != nil {
		return nil, err
	}
	return vmangos.Emotes(dataDir)
}

// SpawnTeleports lists where a creature (kind "creature") or game object (kind "object") spawn sends
// a player or moves itself: its gossip, the spells it casts and the scripts it runs (#306, #308).
func (a *App) SpawnTeleports(kind string, entry, guid int) ([]vmangos.SpawnTeleport, error) {
	server, _, err := a.serverAndData()
	if err != nil {
		return nil, err
	}
	return vmangos.SpawnTeleports(server, kind, entry, guid)
}

// SpawnScripts lists the scripts attached to a creature (kind "creature") or game object (kind
// "object") spawn in the world database: its template's C++ script and AI, an object's use and event
// scripts, a creature's EventAI events and path scripts (#137).
func (a *App) SpawnScripts(kind string, entry, guid int) ([]vmangos.SpawnScript, error) {
	server, _, err := a.serverAndData()
	if err != nil {
		return nil, err
	}
	return vmangos.SpawnScripts(server, kind, entry, guid)
}

// ScriptCommands lists the commands of script id in one of the script tables SpawnScripts names
// (#736).
func (a *App) ScriptCommands(table string, id int) ([]vmangos.ScriptCommand, error) {
	server, _, err := a.serverAndData()
	if err != nil {
		return nil, err
	}
	return vmangos.ScriptCommands(server, table, id)
}

// NextObjectGUID is the first free game object guid of the world database, for placed objects.
func (a *App) NextObjectGUID() (int, error) {
	if realm, err := a.lyraRealm(); err != nil || realm != nil {
		return lyraPlaceholderGUID, err
	}
	server, _, err := a.serverAndData()
	if err != nil {
		return 0, err
	}
	return vmangos.NextObjectGUID(server)
}

// NextPoolEntry is the first spawn pool entry the world database doesn't use yet, for a pool made
// in the editor.
func (a *App) NextPoolEntry() (int, error) {
	server, _, err := a.serverAndData()
	if err != nil {
		return 0, err
	}
	return vmangos.NextPoolEntry(server)
}

// NextCreatureGUID is the first creature guid the world database doesn't use yet, for creatures
// placed in the editor.
func (a *App) NextCreatureGUID() (int, error) {
	if realm, err := a.lyraRealm(); err != nil || realm != nil {
		return lyraPlaceholderGUID, err
	}
	server, _, err := a.serverAndData()
	if err != nil {
		return 0, err
	}
	return vmangos.NextCreatureGUID(server)
}

// CheckSpawnIDs tells File › Check Map what the world database has under the unsaved spawns' guids
// and entries (#195).
func (a *App) CheckSpawnIDs(q vmangos.SpawnIDQuery) (vmangos.SpawnIDReport, error) {
	// A LyraCore realm gives new spawns their guids, so no unsaved guid can clash.
	if realm, err := a.lyraRealm(); err != nil || realm != nil {
		return vmangos.SpawnIDReport{}, err
	}
	server, _, err := a.serverAndData()
	if err != nil {
		return vmangos.SpawnIDReport{}, err
	}
	return vmangos.CheckSpawnIDs(server, q)
}

// ModelAnimations lists the animations an M2 model has, for the creature editor's preview.
func (a *App) ModelAnimations(model string) ([]vmangos.AnimInfo, error) {
	dataDir, err := a.clientDataDir()
	if err != nil {
		return nil, err
	}
	return vmangos.ModelAnimations(dataDir, model)
}

// SpawnEditSQL turns a region's spawn edits into SQL for the world database, without running it.
func (a *App) SpawnEditSQL(mapName string, x, y, size int, edits []vmangos.SpawnEdit) (vmangos.Result, error) {
	if realm, err := a.lyraRealm(); err != nil || realm != nil {
		if err != nil {
			return vmangos.Result{}, err
		}
		return vmangos.Result{}, errors.New("a LyraCore realm has no SQL to export; Save Spawns writes the edits into it")
	}
	server, _, err := a.serverAndData()
	if err != nil {
		return vmangos.Result{}, err
	}
	return vmangos.SpawnSQL(server, mapName, x, y, size, edits, time.Now())
}

// ApplySpawnEdits writes a region's spawn edits into the world database, or into the LyraCore
// realm when one is set, which shows them at once. What was run (the SQL, or the realm's reducer
// argument) is kept in <user cache>/Meld/spawn-sql, so what changed can be looked up.
func (a *App) ApplySpawnEdits(mapName string, x, y, size int, edits []vmangos.SpawnEdit) (vmangos.Result, error) {
	if realm, dataDir, err := a.lyraRealmAndData(); err != nil || realm != nil {
		if err != nil {
			return vmangos.Result{}, err
		}
		res, err := vmangos.ApplyLyraSpawnEdits(realm, dataDir, mapName, x, y, size, edits)
		keepSpawnRecord(mapName, ".json", res.SQL)
		return res, err
	}
	server, _, err := a.serverForWrite()
	if err != nil {
		return vmangos.Result{}, err
	}
	// A new spawn whose guid another spawn took since must not replace that spawn (#199).
	if err := vmangos.CheckCreatedGUIDs(server, edits); err != nil {
		return vmangos.Result{}, err
	}
	res, err := vmangos.SpawnSQL(server, mapName, x, y, size, edits, time.Now())
	if err != nil {
		return res, err
	}
	keepSpawnRecord(mapName, ".sql", res.SQL)
	return res, vmangos.ApplySQL(server, res.SQL)
}

// keepSpawnRecord keeps what a spawn save ran in <user cache>/Meld/spawn-sql; nothing for none.
func keepSpawnRecord(mapName, ext, text string) {
	if text == "" {
		return
	}
	if cache, err := os.UserCacheDir(); err == nil {
		dir := filepath.Join(cache, "Meld", "spawn-sql")
		if os.MkdirAll(dir, 0o755) == nil {
			_ = os.WriteFile(filepath.Join(dir, time.Now().Format("20060102-150405")+"-"+mapName+ext), []byte(text), 0o644)
		}
	}
}
