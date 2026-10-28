package vmangos

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"Meld/internal/stdb"
)

// Read docs/architecture/client.md, "LyraCore creatures", before changing these field mappings.
type lyraCreatureFields struct {
	Name             string  `json:"name" editor:"name"`
	Subname          string  `json:"subname" editor:"subname"`
	DisplayID        uint32  `json:"display_id"`
	Level            uint32  `json:"level" editor:"level_min"`
	Health           uint32  `json:"health" editor:"health_min"`
	FactionTemplate  uint32  `json:"faction_template" editor:"faction"`
	NPCFlags         uint32  `json:"npc_flags" editor:"npc_flags"`
	UnitFlags        uint32  `json:"unit_flags"`
	CreatureType     uint8   `json:"creature_type" editor:"type"`
	CreatureFamily   uint8   `json:"creature_family" editor:"pet_family"`
	TypeFlags        uint32  `json:"type_flags"`
	Rank             uint8   `json:"rank" editor:"rank"`
	Scale            float32 `json:"scale"`
	BaseAttackTimeMS uint32  `json:"base_attack_time_ms" editor:"base_attack_time"`
	MoneyMin         uint32  `json:"money_min" editor:"gold_min"`
	MoneyMax         uint32  `json:"money_max" editor:"gold_max"`
	MaxLevel         uint32  `json:"max_level" editor:"level_max"`
	MaxLevelHealth   uint32  `json:"max_level_health" editor:"health_max"`
	AggroRange       uint32  `json:"aggro_range"`
	DamageMin        uint32  `json:"damage_min" editor:"damage_min"`
	DamageMax        uint32  `json:"damage_max" editor:"damage_max"`
	Armor            uint32  `json:"armor" editor:"armor"`
	PickpocketLootID uint32  `json:"pickpocket_loot_id"`
	SkinLootID       uint32  `json:"skin_loot_id" editor:"skinning_loot_id"`
	TrainerType      uint8   `json:"trainer_type" editor:"trainer_type"`
	TrainerClass     uint8   `json:"trainer_class" editor:"trainer_class"`
}

type lyraCreatureRow struct {
	Entry  uint32
	Fields lyraCreatureFields
}

func lyraCreatureRows(table stdb.Table) ([]lyraCreatureRow, error) {
	out := make([]lyraCreatureRow, 0, len(table.Rows))
	for _, raw := range table.Rows {
		fields := map[string]json.RawMessage{}
		for i, name := range table.Columns {
			if i >= len(raw) {
				return nil, fmt.Errorf("creature row is missing %s", name)
			}
			fields[name] = raw[i]
		}
		var row lyraCreatureRow
		if err := json.Unmarshal(fields["entry"], &row.Entry); err != nil {
			return nil, err
		}
		t := reflect.TypeOf(row.Fields)
		for i := 0; i < t.NumField(); i++ {
			name := t.Field(i).Tag.Get("json")
			if _, ok := fields[name]; !ok {
				return nil, fmt.Errorf("creature %d is missing %s", row.Entry, name)
			}
		}
		b, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &row.Fields); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func lyraCreatureRecord(entry int, fields lyraCreatureFields) TemplateRecord {
	r := TemplateRecord{Realm: true, Entry: entry, Columns: []TemplateColumn{}, Values: map[string]string{}}
	v := reflect.ValueOf(fields)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Tag.Get("editor")
		if name == "" {
			continue
		}
		f := v.Field(i)
		c := TemplateColumn{Name: name}
		switch f.Kind() {
		case reflect.String:
			c.Kind, c.MaxLen = "text", 255
			r.Values[name] = f.String()
		case reflect.Uint8, reflect.Uint32:
			c.Kind, c.Max = "uint", 1<<f.Type().Bits()-1
			r.Values[name] = strconv.FormatUint(f.Uint(), 10)

		}
		r.Columns = append(r.Columns, c)
	}
	return r
}

func lyraCreatureChanges(current lyraCreatureFields, changes map[string]string) (lyraCreatureFields, []string, error) {
	out := current
	record := lyraCreatureRecord(0, current)
	indices := map[string]int{}
	v := reflect.ValueOf(&out).Elem()
	for i := 0; i < v.NumField(); i++ {
		if name := v.Type().Field(i).Tag.Get("editor"); name != "" {
			indices[name] = i
		}
	}
	var skipped []string
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		value := changes[name]
		if old, ok := record.Values[name]; ok && old == value {
			continue
		}
		i, ok := indices[name]
		if !ok {
			skipped = append(skipped, name+": LyraCore has no editable equivalent")
			continue
		}
		f := v.Field(i)
		var err error
		switch f.Kind() {
		case reflect.String:
			if (name == "name" && strings.TrimSpace(value) == "") || len(value) > 255 || strings.ContainsRune(value, 0) {
				return current, nil, refuse(name, "name needs text; name and subname allow no NUL and at most 255 UTF-8 bytes")
			}
			f.SetString(value)
		case reflect.Uint8, reflect.Uint32:
			var n uint64
			n, err = strconv.ParseUint(value, 10, f.Type().Bits())
			f.SetUint(n)

		}
		if err != nil {
			return current, nil, refuse(name, "%s", err.Error())
		}
	}
	return out, skipped, nil
}
