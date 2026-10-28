package vmangos

import (
	"encoding/json"
	"errors"
	"fmt"

	"Meld/internal/lyracore"
	"Meld/internal/stdb"
)

type lyraCreatureCreate struct {
	Fields lyraCreatureFields `json:"fields"`
}
type lyraCreatureUpdate struct {
	Entry  uint32             `json:"entry"`
	Fields lyraCreatureFields `json:"fields"`
}
type lyraCreatureEdit struct {
	Create   *lyraCreatureCreate    `json:"createCreature,omitempty"`
	Update   *lyraCreatureUpdate    `json:"updateCreature,omitempty"`
	Displays *lyraCreatureDisplays  `json:"setCreatureDisplays,omitempty"`
	Spells   *lyraCreatureSpellList `json:"setCreatureSpells,omitempty"`
	Cast     *lyraCreatureCast      `json:"setCreatureCast,omitempty"`
}

func readLyraCreature(realm *lyracore.Realm, entry int) (lyraCreatureFields, error) {
	table, err := realm.DB.Query(realm.Database, fmt.Sprintf("SELECT * FROM game_creature_template WHERE entry = %d", entry))
	if err != nil {
		return lyraCreatureFields{}, err
	}
	rows, err := lyraCreatureRows(table)
	if err != nil {
		return lyraCreatureFields{}, err
	}
	if len(rows) != 1 {
		return lyraCreatureFields{}, fmt.Errorf("the realm has no creature %d", entry)
	}
	return rows[0].Fields, nil
}

func ReadLyraCreature(realm *lyracore.Realm, entry int) (TemplateRecord, error) {
	f, err := readLyraCreature(realm, entry)
	if err != nil {
		return TemplateRecord{}, err
	}
	displays, err := readLyraCreatureDisplays(realm, entry)
	if err != nil {
		return TemplateRecord{}, err
	}
	r := lyraCreatureRecord(entry, f)
	addLyraDisplayColumns(&r, effectiveLyraDisplays(f, displays))
	return r, nil
}

func applyLyraCreatureEdits(realm *lyracore.Realm, edits []lyraCreatureEdit, res Result) (Result, error) {
	res.Live = true
	if len(edits) > 64 {
		return res, refuse("", "a batch holds at most 64 creature edits")
	}
	total := 0
	for _, e := range edits {
		n := 0
		if e.Displays != nil {
			n += len(e.Displays.Displays)
		}
		if e.Spells != nil {
			n += len(e.Spells.Spells)
		}
		if n > 128 {
			return res, refuse("", "a list holds at most 128 rows")
		}
		total += n
	}
	if total > 512 {
		return res, refuse("", "a batch holds at most 512 list rows")
	}
	if len(edits) == 0 {
		return res, nil
	}
	b, err := json.Marshal(edits)
	if err != nil {
		return res, err
	}
	res.SQL = string(b)
	if err := realm.DB.Call(realm.Database, "operator_apply_creature_edits", edits); err != nil {
		var response *stdb.HTTPError
		if errors.As(err, &response) && response.StatusCode == stdb.ReducerFailedStatus {
			return res, refuse("", "%s", err.Error())
		}
		return res, err
	}
	templatesMu.Lock()
	clear(lyraCreatures)
	templatesMu.Unlock()
	forgetDisplays()
	return res, nil
}

func SaveLyraCreature(realm *lyracore.Realm, dataDir string, entry int, values map[string]string) (Result, error) {
	current, err := readLyraCreature(realm, entry)
	if err != nil {
		return Result{}, err
	}
	templateValues, displayValues := map[string]string{}, map[string]string{}
	for k, v := range values {
		if isLyraDisplayColumn(k) {
			displayValues[k] = v
		} else {
			templateValues[k] = v
		}
	}
	fields, skipped, err := lyraCreatureChanges(current, templateValues)
	res := Result{Live: true, Skipped: skipped}
	if err != nil {
		return res, err
	}
	edits := []lyraCreatureEdit{}
	if fields != current {
		edits = append(edits, lyraCreatureEdit{Update: &lyraCreatureUpdate{uint32(entry), fields}})
	}
	if len(displayValues) > 0 {
		displays, err := readLyraCreatureDisplays(realm, entry)
		if err != nil {
			return res, err
		}
		edit, err := lyraDisplayChanges(realm, dataDir, entry, current, displays, displayValues)
		if err != nil {
			return res, err
		}
		if edit != nil {
			edits = append(edits, lyraCreatureEdit{Displays: edit})
		}
	}
	if len(edits) > 0 {
		res.Changed = 1
	}
	return applyLyraCreatureEdits(realm, edits, res)
}

func CreateLyraCreature(realm *lyracore.Realm, base int, name string) (int, error) {
	fields := lyraCreatureFields{Name: name, DisplayID: 49, Level: 1, MaxLevel: 1, Health: 100, MaxLevelHealth: 100, FactionTemplate: 35, CreatureType: 7, Scale: 1, BaseAttackTimeMS: 2000, DamageMin: 1, DamageMax: 2}
	var copiedDisplays []lyraCreatureDisplay
	var copiedSpells []lyraCreatureSpell
	var copiedCast *lyraSomeSpell
	if base != 0 {
		var err error
		fields, err = readLyraCreature(realm, base)
		if err != nil {
			return 0, err
		}
		displays, err := readLyraCreatureDisplays(realm, base)
		if err != nil {
			return 0, err
		}
		if len(displays) > 0 && displays[0].DisplayID == fields.DisplayID && displays[0].Scale == fields.Scale {
			copiedDisplays = displays
		}
		copiedSpells, copiedCast, err = readLyraSpellRows(realm, base)
		if err != nil {
			return 0, err
		}
		if len(copiedDisplays) > 128 || len(copiedSpells) > 128 {
			return 0, refuse("", "the base creature has more than 128 rows in a display or spell list")
		}
		fields.Name = name
	}
	if _, err := applyLyraCreatureEdits(realm, []lyraCreatureEdit{{Create: &lyraCreatureCreate{fields}}}, Result{Added: 1}); err != nil {
		return 0, err
	}
	t, err := realm.DB.Query(realm.Database, "SELECT entry FROM game_creature_template WHERE entry >= 5200500 AND entry <= 5999999")
	if err != nil {
		return 0, fmt.Errorf("creature created, but its entry could not be read: %w", err)
	}
	var entry uint32
	if err := t.Each(func(r *stdb.Row) error { entry = max(entry, uint32(r.Uint("entry"))); return nil }); err != nil {
		return 0, err
	}
	if entry == 0 {
		return 0, fmt.Errorf("creature created, but the Operator Template Range is empty")
	}
	if base != 0 {
		edits := []lyraCreatureEdit{{Spells: &lyraCreatureSpellList{CreatureEntry: entry, Spells: copiedSpells}}, {Cast: &lyraCreatureCast{CreatureEntry: entry, SpellID: copiedCast}}}
		if len(copiedDisplays) > 0 {
			edits = append(edits, lyraCreatureEdit{Displays: &lyraCreatureDisplays{Entry: entry, Displays: copiedDisplays}})
		}
		if _, err := applyLyraCreatureEdits(realm, edits, Result{Changed: 1}); err != nil {
			return 0, fmt.Errorf("created creature %d, but copying its displays and spells failed: %w", entry, err)
		}
	}
	return int(entry), nil
}

func ListLyraDisplays(realm *lyracore.Realm, dataDir string) ([]DisplayEntry, error) {
	templates, err := realm.DB.Query(realm.Database, "SELECT entry, display_id, creature_type, name FROM game_creature_template")
	if err != nil {
		return nil, err
	}
	displays, err := realm.DB.Query(realm.Database, "SELECT entry, display_id FROM game_creature_display")
	if err != nil {
		return nil, err
	}
	byEntry := map[uint64]map[int]bool{}
	if err := displays.Each(func(r *stdb.Row) error {
		entry := r.Uint("entry")
		if byEntry[entry] == nil {
			byEntry[entry] = map[int]bool{}
		}
		byEntry[entry][int(r.Uint("display_id"))] = true
		return nil
	}); err != nil {
		return nil, err
	}
	uses := map[int]*displayUse{}
	err = templates.Each(func(r *stdb.Row) error {
		ids := byEntry[r.Uint("entry")]
		if ids == nil {
			ids = map[int]bool{}
		}
		ids[int(r.Uint("display_id"))] = true
		for id := range ids {
			if id == 0 {
				continue
			}
			if uses[id] == nil {
				uses[id] = &displayUse{types: map[int]int{}}
			}
			uses[id].types[int(r.Uint("creature_type"))]++
			uses[id].names = append(uses[id].names, r.String("name"))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	tables, err := cachedModelTables(dataDir)
	if err != nil {
		return nil, err
	}
	return listDisplays(tables, uses), nil
}
