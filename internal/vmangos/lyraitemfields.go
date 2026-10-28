package vmangos

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"Meld/internal/stdb"
)

// The editor tags invert LyraCore's importer/src/world_import.rs item translation.
type lyraItemFields struct {
	Class                     uint8   `json:"class" editor:"class"`
	Subclass                  uint8   `json:"subclass" editor:"subclass"`
	Name                      string  `json:"name" editor:"name"`
	DisplayID                 uint32  `json:"display_id" editor:"display_id"`
	Quality                   uint8   `json:"quality" editor:"quality"`
	InventoryType             uint8   `json:"inventory_type" editor:"inventory_type"`
	ItemLevel                 uint8   `json:"item_level" editor:"item_level"`
	RequiredLevel             uint8   `json:"required_level" editor:"required_level"`
	MaxDurability             uint32  `json:"max_durability" editor:"max_durability"`
	BuyPrice                  uint32  `json:"buy_price" editor:"buy_price"`
	SellPrice                 uint32  `json:"sell_price" editor:"sell_price"`
	MaxStack                  uint32  `json:"max_stack" editor:"stackable"`
	DamageMin                 float32 `json:"damage_min" editor:"dmg_min1"`
	DamageMax                 float32 `json:"damage_max" editor:"dmg_max1"`
	DelayMS                   uint32  `json:"delay_ms" editor:"delay"`
	StatStrength              int32   `json:"stat_strength"`
	StatAgility               int32   `json:"stat_agility"`
	StatStamina               int32   `json:"stat_stamina"`
	StatIntellect             int32   `json:"stat_intellect"`
	StatSpirit                int32   `json:"stat_spirit"`
	StatCrit                  int32   `json:"stat_crit"`
	StatHit                   int32   `json:"stat_hit"`
	StatArmor                 int32   `json:"stat_armor" editor:"armor"`
	BlockValue                int32   `json:"block_value" editor:"block"`
	RestoresPower             bool    `json:"restores_power"`
	SpellID1                  uint32  `json:"spellid_1" editor:"spellid_1"`
	SpellTrigger1             uint8   `json:"spelltrigger_1" editor:"spelltrigger_1"`
	SpellID2                  uint32  `json:"spellid_2" editor:"spellid_2"`
	SpellTrigger2             uint8   `json:"spelltrigger_2" editor:"spelltrigger_2"`
	ContainerSlots            uint8   `json:"container_slots" editor:"container_slots"`
	Sheath                    uint8   `json:"sheath" editor:"sheath"`
	Bonding                   uint8   `json:"bonding" editor:"bonding"`
	HolyRes                   int32   `json:"holy_res" editor:"holy_res"`
	FireRes                   int32   `json:"fire_res" editor:"fire_res"`
	NatureRes                 int32   `json:"nature_res" editor:"nature_res"`
	FrostRes                  int32   `json:"frost_res" editor:"frost_res"`
	ShadowRes                 int32   `json:"shadow_res" editor:"shadow_res"`
	ArcaneRes                 int32   `json:"arcane_res" editor:"arcane_res"`
	SpellID3                  uint32  `json:"spellid_3" editor:"spellid_3"`
	SpellTrigger3             uint8   `json:"spelltrigger_3" editor:"spelltrigger_3"`
	SpellID4                  uint32  `json:"spellid_4" editor:"spellid_4"`
	SpellTrigger4             uint8   `json:"spelltrigger_4" editor:"spelltrigger_4"`
	SpellID5                  uint32  `json:"spellid_5" editor:"spellid_5"`
	SpellTrigger5             uint8   `json:"spelltrigger_5" editor:"spelltrigger_5"`
	RequiredSkill             uint32  `json:"required_skill" editor:"required_skill"`
	RequiredSkillRank         uint32  `json:"required_skill_rank" editor:"required_skill_rank"`
	RequiredReputationFaction uint32  `json:"required_reputation_faction" editor:"required_reputation_faction"`
	RequiredReputationRank    uint32  `json:"required_reputation_rank" editor:"required_reputation_rank"`
	MaxCount                  uint32  `json:"max_count" editor:"max_count"`
	ItemFlags                 uint32  `json:"item_flags" editor:"flags"`
	PageText                  uint32  `json:"page_text" editor:"page_text"`
	StartQuest                uint32  `json:"start_quest" editor:"start_quest"`
	BagFamily                 uint32  `json:"bag_family" editor:"bag_family"`
	BuyCount                  uint32  `json:"buy_count" editor:"buy_count"`
	FoodType                  uint8   `json:"food_type" editor:"food_type"`
	AllowedClass              uint32  `json:"allowed_class" editor:"allowable_class"`
	AllowedRace               uint32  `json:"allowed_race" editor:"allowable_race"`
	RandomProperty            uint32  `json:"random_property" editor:"random_property"`
}

type lyraItemRow struct {
	Entry  uint32
	Fields lyraItemFields
}

func lyraItemRows(table stdb.Table) ([]lyraItemRow, error) {
	out := make([]lyraItemRow, 0, len(table.Rows))
	for _, raw := range table.Rows {
		fields := map[string]json.RawMessage{}
		for i, name := range table.Columns {
			if i >= len(raw) {
				return nil, fmt.Errorf("item row is missing %s", name)
			}
			fields[name] = raw[i]
		}
		var row lyraItemRow
		if err := json.Unmarshal(fields["entry"], &row.Entry); err != nil {
			return nil, err
		}
		t := reflect.TypeOf(row.Fields)
		for i := 0; i < t.NumField(); i++ {
			name := t.Field(i).Tag.Get("json")
			if _, ok := fields[name]; !ok {
				return nil, fmt.Errorf("item %d is missing %s", row.Entry, name)
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

func lyraItemRecord(entry int, fields lyraItemFields) TemplateRecord {
	r := TemplateRecord{Entry: entry, Columns: []TemplateColumn{}, Values: map[string]string{}}
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
		case reflect.Int32:
			c.Kind, c.Min, c.Max = "int", math.MinInt32, math.MaxInt32
			r.Values[name] = strconv.FormatInt(f.Int(), 10)
		case reflect.Float32:
			c.Kind = "float"
			r.Values[name] = strconv.FormatFloat(f.Float(), 'f', -1, 32)
		}
		if name == "allowable_class" || name == "allowable_race" {
			c.Kind, c.Min = "int", -1
		}
		r.Columns = append(r.Columns, c)
	}
	stats := []int32{fields.StatAgility, fields.StatStrength, fields.StatIntellect, fields.StatSpirit, fields.StatStamina}
	for i := 1; i <= 10; i++ {
		t, v := fmt.Sprintf("stat_type%d", i), fmt.Sprintf("stat_value%d", i)
		r.Columns = append(r.Columns, TemplateColumn{Name: t, Kind: "uint", Max: 7}, TemplateColumn{Name: v, Kind: "int", Min: math.MinInt32, Max: math.MaxInt32})
		r.Values[t], r.Values[v] = "0", "0"
		if i <= len(stats) {
			r.Values[t], r.Values[v] = strconv.Itoa(i+2), strconv.FormatInt(int64(stats[i-1]), 10)
		}
	}
	return r
}

func lyraItemChanges(current lyraItemFields, changes map[string]string) (lyraItemFields, []string, error) {
	out := current
	record := lyraItemRecord(0, current)
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
	statsChanged := false
	for _, name := range keys {
		value := changes[name]
		if old, ok := record.Values[name]; ok && old == value {
			continue
		}
		if strings.HasPrefix(name, "stat_type") || strings.HasPrefix(name, "stat_value") {
			if _, ok := record.Values[name]; ok {
				record.Values[name] = value
				statsChanged = true
				continue
			}
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
			if strings.TrimSpace(value) == "" || len(value) > 255 || strings.ContainsRune(value, 0) {
				return current, nil, refuse(name, "an item name needs text, no NUL, and at most 255 UTF-8 bytes")
			}
			f.SetString(value)
		case reflect.Uint8, reflect.Uint32:
			if value == "-1" && name == "allowable_class" {
				value = "1503"
			}
			if value == "-1" && name == "allowable_race" {
				value = "255"
			}
			var n uint64
			n, err = strconv.ParseUint(value, 10, f.Type().Bits())
			f.SetUint(n)
		case reflect.Int32:
			var n int64
			n, err = strconv.ParseInt(value, 10, 32)
			f.SetInt(n)
		case reflect.Float32:
			var n float64
			n, err = strconv.ParseFloat(value, 32)
			if math.IsNaN(n) || math.IsInf(n, 0) {
				err = fmt.Errorf("damage must be finite")
			}
			f.SetFloat(n)
		}
		if err != nil {
			return current, nil, refuse(name, "%s", err.Error())
		}
	}
	if statsChanged {
		var stats [8]int64
		valid := true
		for i := 1; i <= 10; i++ {
			t, v := fmt.Sprintf("stat_type%d", i), fmt.Sprintf("stat_value%d", i)
			kind, err := strconv.Atoi(record.Values[t])
			if err != nil {
				return current, nil, refuse(t, "stat type must be an integer")
			}
			n, err := strconv.ParseInt(record.Values[v], 10, 32)
			if err != nil {
				return current, nil, refuse(v, "stat value must fit a signed 32-bit integer")
			}
			if kind == 0 && n == 0 {
				continue
			}
			if kind < 3 || kind > 7 {
				skipped = append(skipped, fmt.Sprintf("stat slot %d: type %d has no LyraCore stat column; no primary stats written", i, kind))
				valid = false
				continue
			}
			stats[kind] += n
		}
		for _, n := range stats {
			if n < math.MinInt32 || n > math.MaxInt32 {
				return current, nil, refuse("stat_value1", "the sum for a stat must fit a signed 32-bit integer")
			}
		}
		if valid {
			out.StatAgility, out.StatStrength, out.StatIntellect, out.StatSpirit, out.StatStamina = int32(stats[3]), int32(stats[4]), int32(stats[5]), int32(stats[6]), int32(stats[7])
		}
	}
	return out, skipped, nil
}

func newLyraItem(name string) lyraItemFields {
	return lyraItemFields{Name: name, Class: 15, Quality: 1, MaxStack: 1, BuyCount: 1, AllowedClass: 0x5df, AllowedRace: 0xff}
}
