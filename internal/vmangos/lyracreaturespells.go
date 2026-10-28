package vmangos

import (
	"fmt"
	"math"
	"reflect"
	"sort"

	"Meld/internal/lyracore"
	"Meld/internal/stdb"
)

type lyraCreatureSpell struct {
	SpellID        uint32 `json:"spell_id"`
	Priority       uint8  `json:"priority"`
	Condition      uint8  `json:"condition"`
	ConditionValue uint8  `json:"condition_value"`
}
type lyraCreatureSpellList struct {
	CreatureEntry uint32              `json:"creature_entry"`
	Spells        []lyraCreatureSpell `json:"spells"`
}
type lyraCreatureCast struct {
	CreatureEntry uint32         `json:"creature_entry"`
	SpellID       *lyraSomeSpell `json:"spell_id"`
}
type lyraSomeSpell struct {
	Some uint32 `json:"some"`
}

func lyraSpellEdits(entry int, spells []CreatureSpell) ([]lyraCreatureEdit, []string, error) {
	if len(spells) > creatureSpellSlots {
		return nil, nil, refuse("", "Meld supports at most eight spells")
	}
	list := &lyraCreatureSpellList{CreatureEntry: uint32(entry), Spells: []lyraCreatureSpell{}}
	cast := &lyraCreatureCast{CreatureEntry: uint32(entry)}
	skipped := []string{}
	for i, s := range spells {
		if s.Spell <= 0 || uint64(s.Spell) > math.MaxUint32 {
			return nil, nil, refuse("", "spell must fit a positive u32")
		}
		if i == 0 {
			cast.SpellID = &lyraSomeSpell{uint32(s.Spell)}
		}
		if len(spells) > 1 {
			row := lyraCreatureSpell{SpellID: uint32(s.Spell), Priority: 20, Condition: 3}
			if i == 0 {
				row.Priority, row.Condition = 10, 0
			}
			list.Spells = append(list.Spells, row)
		}
		for _, extra := range []struct {
			name  string
			value int
		}{{"probability", s.Probability}, {"castTarget", s.CastTarget}, {"targetParam1", s.TargetParam1}, {"targetParam2", s.TargetParam2}, {"castFlags", s.CastFlags}, {"delayInitialMin", s.InitialMin}, {"delayInitialMax", s.InitialMax}, {"delayRepeatMin", s.RepeatMin}, {"delayRepeatMax", s.RepeatMax}, {"scriptId", s.ScriptID}} {
			if extra.value != 0 {
				skipped = append(skipped, fmt.Sprintf("spell %d %s: LyraCore has no equivalent", s.Spell, extra.name))
			}
		}
	}
	return []lyraCreatureEdit{{Spells: list}, {Cast: cast}}, skipped, nil
}

func readLyraSpellRows(realm *lyracore.Realm, entry int) ([]lyraCreatureSpell, *lyraSomeSpell, error) {
	t, err := realm.DB.Query(realm.Database, fmt.Sprintf("SELECT * FROM game_creature_spell WHERE creature_entry = %d", entry))
	if err != nil {
		return nil, nil, err
	}
	type row struct {
		id    uint64
		spell lyraCreatureSpell
	}
	rows := []row{}
	if err := t.Each(func(r *stdb.Row) error {
		rows = append(rows, row{r.Uint("id"), lyraCreatureSpell{uint32(r.Uint("spell_id")), uint8(r.Uint("priority")), uint8(r.Uint("condition")), uint8(r.Uint("condition_value"))}})
		return nil
	}); err != nil {
		return nil, nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
	spells := make([]lyraCreatureSpell, 0, len(rows))
	for _, r := range rows {
		spells = append(spells, r.spell)
	}
	t, err = realm.DB.Query(realm.Database, fmt.Sprintf("SELECT spell_id FROM game_creature_cast WHERE creature_entry = %d", entry))
	if err != nil {
		return nil, nil, err
	}
	var cast *lyraSomeSpell
	err = t.Each(func(r *stdb.Row) error { cast = &lyraSomeSpell{uint32(r.Uint("spell_id"))}; return nil })
	return spells, cast, err
}

func ReadLyraCreatureSpells(realm *lyracore.Realm, entry int) (CreatureSpells, error) {
	f, err := readLyraCreature(realm, entry)
	if err != nil {
		return CreatureSpells{}, err
	}
	rows, cast, err := readLyraSpellRows(realm, entry)
	if err != nil {
		return CreatureSpells{}, err
	}
	out := CreatureSpells{Name: f.Name, Shared: 1, Spells: []CreatureSpell{}}
	for _, s := range rows {
		out.Spells = append(out.Spells, CreatureSpell{Spell: int(s.SpellID)})
	}
	if len(rows) == 0 && cast != nil {
		out.Spells = append(out.Spells, CreatureSpell{Spell: int(cast.Some)})
	}
	if len(out.Spells) > 0 {
		out.ListID = entry
	}
	edits, _, err := lyraSpellEdits(entry, out.Spells)
	if err != nil || !reflect.DeepEqual(edits[0].Spells.Spells, rows) || !reflect.DeepEqual(edits[1].Cast.SpellID, cast) {
		out.Unavailable = "This realm spell list has priorities, conditions or a fallback cast that Meld cannot represent. It is read-only."
	}
	return out, nil
}

func SaveLyraCreatureSpells(realm *lyracore.Realm, entry int, spells []CreatureSpell) (Result, error) {
	current, err := ReadLyraCreatureSpells(realm, entry)
	if err != nil {
		return Result{}, err
	}
	if current.Unavailable != "" {
		return Result{}, refuse("", "%s", current.Unavailable)
	}
	edits, skipped, err := lyraSpellEdits(entry, spells)
	if err != nil {
		return Result{}, err
	}
	return applyLyraCreatureEdits(realm, edits, Result{Changed: 1, Skipped: skipped})
}
