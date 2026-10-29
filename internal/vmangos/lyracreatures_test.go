package vmangos

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"Meld/internal/lyracore"
	"Meld/internal/mpq"
	"Meld/internal/stdb"
)

func lyraTestCreature() lyraCreatureFields {
	return lyraCreatureFields{Name: "Forest keeper", Subname: "Watch", DisplayID: 100, Level: 10, Health: 250, FactionTemplate: 35, NPCFlags: 129, UnitFlags: 0x2000040, CreatureType: 7, CreatureFamily: 1, TypeFlags: 4, Rank: 1, Scale: 1.25, BaseAttackTimeMS: 2000, MoneyMin: 1, MoneyMax: 12, MaxLevel: 12, MaxLevelHealth: 320, AggroRange: 17, DamageMin: 5, DamageMax: 10, Armor: 50, PickpocketLootID: 22, SkinLootID: 42, TrainerType: 2, TrainerClass: 8}
}

func creatureTableForTest(t *testing.T, rows ...any) stdb.Table {
	t.Helper()
	table := stdb.Table{Rows: [][]json.RawMessage{}}
	for _, row := range rows {
		b, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(b, &fields); err != nil {
			t.Fatal(err)
		}
		if len(table.Columns) == 0 {
			for name := range fields {
				table.Columns = append(table.Columns, name)
			}
			sort.Strings(table.Columns)
		}
		raw := []json.RawMessage{}
		for _, name := range table.Columns {
			raw = append(raw, fields[name])
		}
		table.Rows = append(table.Rows, raw)
	}
	return table
}

func TestLyraCreatureMapping(t *testing.T) {
	want := lyraTestCreature()
	table := creatureTableForTest(t, struct {
		Entry uint32 `json:"entry"`
		lyraCreatureFields
	}{5200500, want})
	rows, err := lyraCreatureRows(table)
	if err != nil || len(rows) != 1 || rows[0].Fields != want {
		t.Fatalf("rows: %+v %v", rows, err)
	}
	record := lyraCreatureRecord(5200500, want)
	for col, v := range map[string]string{"name": "Forest keeper", "subname": "Watch", "level_min": "10", "level_max": "12", "health_min": "250", "health_max": "320", "faction": "35", "type": "7", "pet_family": "1", "gold_min": "1", "gold_max": "12", "base_attack_time": "2000", "skinning_loot_id": "42", "damage_min": "5", "damage_max": "10", "armor": "50"} {
		if record.Values[col] != v {
			t.Errorf("%s = %q, want %q", col, record.Values[col], v)
		}
	}
	got, skipped, err := lyraCreatureChanges(want, record.Values)
	if err != nil || len(skipped) != 0 || got != want {
		t.Fatalf("round trip: %+v %v %v", got, skipped, err)
	}
	got, skipped, err = lyraCreatureChanges(want, map[string]string{"name": "New name", "npc_flags": "3", "health_min": "300", "health_max": "400", "equipment_id": "4", "health_multiplier": "3", "static_flags1": "64", "pickpocket_loot_id": "2"})
	want.Name, want.NPCFlags, want.Health, want.MaxLevelHealth = "New name", 3, 300, 400
	if err != nil || len(skipped) != 4 || got != want {
		t.Fatalf("changes: %+v %v %v", got, skipped, err)
	}
	for _, col := range record.Columns {
		switch col.Name {
		case "unit_flags", "type_flags", "static_flags1", "health_multiplier", "equipment_id", "spell_list_id", "pickpocket_loot_id":
			t.Errorf("unsupported editable field %s", col.Name)
		case "name":
			if col.Kind != "text" || col.MaxLen != 255 {
				t.Errorf("name metadata: %+v", col)
			}
		case "health_min":
			if col.Kind != "uint" || col.Max != 4294967295 {
				t.Errorf("health metadata: %+v", col)
			}
		}
	}
	for _, changes := range []map[string]string{{"name": " "}, {"name": strings.Repeat("é", 128)}, {"subname": "\x00"}, {"level_min": "-1"}, {"type": "256"}, {"health_min": "4294967296"}} {
		if _, _, err := lyraCreatureChanges(want, changes); Refused(err) == nil {
			t.Errorf("not refused: %v, %v", changes, err)
		}
	}
	got, _, err = lyraCreatureChanges(want, map[string]string{"subname": ""})
	if err != nil || got.Subname != "" {
		t.Fatal("subname cannot be cleared")
	}
	table.Columns = table.Columns[1:]
	table.Rows[0] = table.Rows[0][1:]
	if _, err := lyraCreatureRows(table); err == nil {
		t.Fatal("accepted incomplete full row")
	}
}

type creatureRealmTest struct {
	fields   lyraCreatureFields
	displays []lyraCreatureDisplay
	spells   []lyraCreatureSpell
	cast     *lyraSomeSpell
	calls    [][]lyraCreatureEdit
}

func newCreatureRealmTest(t *testing.T) (*lyracore.Realm, *creatureRealmTest) {
	t.Helper()
	state := &creatureRealmTest{fields: lyraTestCreature(), displays: []lyraCreatureDisplay{{100, 1.25, 1, 1, 0.5}, {200, 1.5, 3, 2, 0.7}}, spells: []lyraCreatureSpell{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/sql") {
			query := string(b)
			if !strings.HasPrefix(query, "SELECT ") {
				t.Errorf("SQL write: %s", query)
				w.WriteHeader(500)
				return
			}
			var rows []any
			switch {
			case strings.Contains(query, "FROM game_creature_template"):
				rows = append(rows, struct {
					Entry uint32 `json:"entry"`
					lyraCreatureFields
				}{5200500, state.fields})
			case strings.Contains(query, "FROM game_creature_display"):
				for i, d := range state.displays {
					if strings.Contains(query, "WHERE display_id") && !strings.Contains(query, fmt.Sprintf("= %d ", d.DisplayID)) {
						continue
					}
					rows = append(rows, struct {
						ID    int    `json:"id"`
						Entry uint32 `json:"entry"`
						lyraCreatureDisplay
					}{i + 1, 5200500, d})
				}
			case strings.Contains(query, "FROM game_creature_spell"):
				for i, s := range state.spells {
					rows = append(rows, struct {
						ID int `json:"id"`
						lyraCreatureSpell
					}{i + 1, s})
				}
			case strings.Contains(query, "FROM game_creature_cast"):
				if state.cast != nil {
					rows = append(rows, map[string]uint32{"spell_id": state.cast.Some})
				}
			default:
				t.Errorf("unexpected SELECT: %s", query)
			}
			writeLyraTable(t, w, creatureTableForTest(t, rows...))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/call/operator_apply_creature_edits") {
			t.Errorf("wrong endpoint: %s", r.URL.Path)
		}
		var args [][]lyraCreatureEdit
		if err := json.Unmarshal(b, &args); err != nil || len(args) != 1 {
			t.Errorf("body %s: %v", b, err)
			w.WriteHeader(500)
			return
		}
		state.calls = append(state.calls, args[0])
		for _, e := range args[0] {
			switch {
			case e.Create != nil:
				state.fields = e.Create.Fields
			case e.Update != nil:
				state.fields = e.Update.Fields
			case e.Displays != nil:
				state.displays = e.Displays.Displays
				if len(state.displays) > 0 {
					state.fields.DisplayID = state.displays[0].DisplayID
					state.fields.Scale = state.displays[0].Scale
				}
			case e.Spells != nil:
				state.spells = e.Spells.Spells
			case e.Cast != nil:
				state.cast = e.Cast.SpellID
			default:
				t.Errorf("empty variant: %s", b)
			}
		}
	}))
	t.Cleanup(server.Close)
	return &lyracore.Realm{DB: &stdb.Client{URL: server.URL}, Database: "test"}, state
}

func TestLyraCreatureSaveFreshRowAndDisplays(t *testing.T) {
	realm, state := newCreatureRealmTest(t)
	record, err := ReadLyraCreature(realm, 5200500)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"display_id1": "100", "display_scale1": "1.25", "display_probability1": "1", "display_id2": "200", "display_scale2": "1.5", "display_probability2": "3", "display_id3": "0", "display_total_probability": "4"} {
		if record.Values[k] != v {
			t.Errorf("%s = %q", k, record.Values[k])
		}
	}
	if _, err := SaveLyraCreature(realm, "", 5200500, record.Values); err != nil || len(state.calls) != 0 {
		t.Fatalf("no-op: %v %+v", err, state.calls)
	}
	state.fields.AggroRange = 77
	displaysMu.Lock()
	displaysCache["test"] = []DisplayEntry{{ID: 1}}
	displaysMu.Unlock()
	templatesMu.Lock()
	lyraCreatures["test"] = []CreatureTemplate{{Entry: 1}}
	templatesMu.Unlock()
	res, err := SaveLyraCreature(realm, "", 5200500, map[string]string{"name": "Changed", "display_id1": "200", "display_scale1": "2", "equipment_id": "3"})
	if err != nil || !res.Live || res.Changed != 1 || len(res.Skipped) != 1 {
		t.Fatalf("save: %+v %v", res, err)
	}
	if len(state.calls) != 1 || len(state.calls[0]) != 2 {
		t.Fatalf("not one transaction: %+v", state.calls)
	}
	if state.fields.AggroRange != 77 || state.fields.Name != "Changed" || state.fields.DisplayID != 200 || state.fields.Scale != 2 {
		t.Fatalf("fields: %+v", state.fields)
	}
	if state.displays[0].Gender != 2 || state.displays[0].BoundingRadius != 0.7 || state.displays[1].Weight != 3 {
		t.Fatalf("lost display fields: %+v", state.displays)
	}
	displaysMu.Lock()
	cached := len(displaysCache)
	displaysMu.Unlock()
	templatesMu.Lock()
	templateCount := len(lyraCreatures)
	templatesMu.Unlock()
	if templateCount != 0 {
		t.Fatal("creature template cache not invalidated")
	}
	if cached != 0 {
		t.Fatal("display cache not invalidated")
	}
	if !strings.Contains(res.SQL, `"updateCreature"`) || !strings.Contains(res.SQL, `"setCreatureDisplays"`) || strings.Contains(res.SQL, "UPDATE ") {
		t.Fatal(res.SQL)
	}
	if _, err := SaveLyraCreature(realm, "", 5200500, map[string]string{"name": "Must not write", "display_scale1": "NaN"}); Refused(err) == nil || len(state.calls) != 1 {
		t.Fatalf("invalid display did not prevent template write: %v", err)
	}
}

func TestLyraCreatureDisplaysImporterRules(t *testing.T) {
	realm, state := newCreatureRealmTest(t)
	res, err := SaveLyraCreature(realm, "", 5200500, map[string]string{"display_total_probability": "0", "display_id2": "0", "display_id3": "300", "display_scale3": "0"})
	if err != nil {
		t.Fatalf("%+v %v", res, err)
	}
	if len(state.displays) != 2 || state.displays[0].Weight != 1 || state.displays[1].Weight != 0 || state.displays[1].Scale != 1 || state.displays[1].BoundingRadius != 0.389 {
		t.Fatalf("default scale/contiguous weights: %+v", state.displays)
	}
	for _, values := range []map[string]string{{"display_scale1": "-1"}, {"display_id1": "-1"}, {"display_total_probability": "99"}, {"display_probability1": "4294967296"}, {"display_id1": "0", "display_id2": "0"}} {
		if _, err := SaveLyraCreature(realm, "", 5200500, values); Refused(err) == nil {
			t.Errorf("not refused: %v %v", values, err)
		}
	}
	state.displays = append(state.displays, state.displays[0], state.displays[0], state.displays[0])
	if _, err := SaveLyraCreature(realm, "", 5200500, map[string]string{"name": "Still editable"}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveLyraCreature(realm, "", 5200500, map[string]string{"display_id1": "900"}); Refused(err) == nil {
		t.Fatal("silently truncated more than four displays")
	}
}

func TestLyraCreatureCopy(t *testing.T) {
	realm, state := newCreatureRealmTest(t)
	state.spells = []lyraCreatureSpell{{133, 10, 0, 0}, {17, 20, 3, 0}}
	state.cast = &lyraSomeSpell{133}
	wantSpells := append([]lyraCreatureSpell{}, state.spells...)
	want := state.fields
	want.Name = "Copy"
	displays := append([]lyraCreatureDisplay{}, state.displays...)
	entry, err := CreateLyraCreature(realm, 42, "Copy")
	if err != nil || entry != 5200500 || state.fields != want || !reflect.DeepEqual(state.displays, displays) {
		t.Fatalf("copy: %d %+v %+v %v", entry, state.fields, state.displays, err)
	}
	if !reflect.DeepEqual(state.spells, wantSpells) || state.cast.Some != 133 {
		t.Fatal("copy lost spells")
	}
	if len(state.calls) != 2 || state.calls[0][0].Create == nil || state.calls[1][2].Displays == nil {
		t.Fatal(state.calls)
	}
}

func TestLyraCreatureSpellMappingAndJSON(t *testing.T) {
	for _, tt := range []struct {
		spells []CreatureSpell
		want   string
	}{
		{nil, `[{"setCreatureSpells":{"creature_entry":5200500,"spells":[]}},{"setCreatureCast":{"creature_entry":5200500,"spell_id":null}}]`},
		{[]CreatureSpell{{Spell: 133}}, `[{"setCreatureSpells":{"creature_entry":5200500,"spells":[]}},{"setCreatureCast":{"creature_entry":5200500,"spell_id":{"some":133}}}]`},
		{[]CreatureSpell{{Spell: 133}, {Spell: 17}, {Spell: 116}}, `[{"setCreatureSpells":{"creature_entry":5200500,"spells":[{"spell_id":133,"priority":10,"condition":0,"condition_value":0},{"spell_id":17,"priority":20,"condition":3,"condition_value":0},{"spell_id":116,"priority":20,"condition":3,"condition_value":0}]}},{"setCreatureCast":{"creature_entry":5200500,"spell_id":{"some":133}}}]`},
	} {
		realm, _ := newCreatureRealmTest(t)
		res, err := SaveLyraCreatureSpells(realm, 5200500, tt.spells)
		if err != nil || res.SQL != tt.want {
			t.Fatalf("JSON: %s %v, want %s", res.SQL, err, tt.want)
		}
		got, err := ReadLyraCreatureSpells(realm, 5200500)
		if err != nil || got.Unavailable != "" || len(got.Spells) != len(tt.spells) {
			t.Fatalf("readback: %+v %v", got, err)
		}
		for i, s := range tt.spells {
			if got.Spells[i] != s {
				t.Fatal(got)
			}
		}
		if (got.ListID != 0) != (len(tt.spells) > 0) {
			t.Fatal(got)
		}
	}
	_, skipped, err := lyraSpellEdits(1, []CreatureSpell{{Spell: 133, Probability: 100, CastTarget: 1, TargetParam1: 2, TargetParam2: 3, CastFlags: 4, InitialMin: 5, InitialMax: 6, RepeatMin: 7, RepeatMax: 8, ScriptID: 9}})
	if err != nil || len(skipped) != 10 {
		t.Fatalf("unsupported: %v %v", skipped, err)
	}
	realm, state := newCreatureRealmTest(t)
	state.spells = []lyraCreatureSpell{{133, 7, 1, 25}}
	got, err := ReadLyraCreatureSpells(realm, 5200500)
	if err != nil || got.Unavailable == "" {
		t.Fatalf("custom conditions: %+v %v", got, err)
	}
	if _, err := SaveLyraCreatureSpells(realm, 5200500, []CreatureSpell{{Spell: 116}}); Refused(err) == nil || len(state.calls) != 0 {
		t.Fatal("custom conditions overwritten")
	}
}

func TestLyraCreatureReducerJSONAndRefusals(t *testing.T) {
	const fields = `{"name":"Forest keeper","subname":"Watch","display_id":100,"level":10,"health":250,"faction_template":35,"npc_flags":129,"unit_flags":33554496,"creature_type":7,"creature_family":1,"type_flags":4,"rank":1,"scale":1.25,"base_attack_time_ms":2000,"money_min":1,"money_max":12,"max_level":12,"max_level_health":320,"aggro_range":17,"damage_min":5,"damage_max":10,"armor":50,"pickpocket_loot_id":22,"skin_loot_id":42,"trainer_type":2,"trainer_class":8}`
	for _, tt := range []struct {
		edit lyraCreatureEdit
		want string
	}{
		{lyraCreatureEdit{Create: &lyraCreatureCreate{Fields: lyraTestCreature()}}, `{"createCreature":{"fields":` + fields + `}}`},
		{lyraCreatureEdit{Update: &lyraCreatureUpdate{Entry: 5200500, Fields: lyraTestCreature()}}, `{"updateCreature":{"entry":5200500,"fields":` + fields + `}}`},
	} {
		b, err := json.Marshal(tt.edit)
		if err != nil || string(b) != tt.want {
			t.Fatalf("template JSON: %s %v, want %s", b, err, tt.want)
		}
	}
	display := lyraCreatureEdit{Displays: &lyraCreatureDisplays{5200500, []lyraCreatureDisplay{{100, 1, 1, 0, 0.389}}}}
	b, err := json.Marshal(display)
	if err != nil || string(b) != `{"setCreatureDisplays":{"entry":5200500,"displays":[{"display_id":100,"scale":1,"weight":1,"gender":0,"bounding_radius":0.389}]}}` {
		t.Fatalf("display JSON: %s %v", b, err)
	}
	for _, status := range []int{stdb.ReducerFailedStatus, http.StatusServiceUnavailable} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "creature edit 0: fixture entry", status) }))
		realm := &lyracore.Realm{DB: &stdb.Client{URL: server.URL}, Database: "test"}
		_, err := applyLyraCreatureEdits(realm, []lyraCreatureEdit{display}, Result{})
		if err == nil || (Refused(err) != nil) != (status == stdb.ReducerFailedStatus) {
			t.Errorf("status %d: %v", status, err)
		}
		server.Close()
	}
	realm, state := newCreatureRealmTest(t)
	if _, err := applyLyraCreatureEdits(realm, make([]lyraCreatureEdit, 65), Result{}); Refused(err) == nil || len(state.calls) != 0 {
		t.Fatal("accepted oversized batch")
	}
}

// Creation leaves the entry in the disposable realm; the reducer has no delete variant.
func TestApplyLyraCreatureEditsLive(t *testing.T) {
	url, file, wow := os.Getenv("LYRA_URL"), os.Getenv("LYRA_TOKEN_FILE"), os.Getenv("WOW_DIR")
	if url == "" || file == "" || wow == "" || os.Getenv("LYRA_THROWAWAY") != "1" {
		t.Skip("set LYRA_URL, LYRA_TOKEN_FILE, WOW_DIR and LYRA_THROWAWAY=1 for a disposable realm")
	}
	if strings.Contains(url, ":3000") {
		t.Fatal("refusing the protected realm port 3000")
	}
	token, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	realm, err := lyracore.Open(lyracore.Settings{URL: url, Token: string(token), Database: os.Getenv("LYRA_DATABASE")})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := CreateLyraCreature(realm, 0, "Meld live creature")
	if err != nil {
		t.Fatal(err)
	}
	res, err := SaveLyraCreature(realm, filepath.Join(wow, "Data"), entry, map[string]string{"name": "Meld live captain", "npc_flags": "129", "display_id1": "100", "display_scale1": "1.5", "display_probability1": "1", "display_id2": "200", "display_scale2": "2", "display_probability2": "3", "display_total_probability": "4"})
	if err != nil || !res.Live {
		t.Fatalf("save: %+v %v", res, err)
	}
	got, err := ReadLyraCreature(realm, entry)
	if err != nil || got.Values["name"] != "Meld live captain" || got.Values["npc_flags"] != "129" || got.Values["display_id2"] != "200" || got.Values["display_scale1"] != "1.5" {
		t.Fatalf("readback: %+v %v", got, err)
	}
	spells, err := realm.DB.Query(realm.Database, "SELECT spell_id FROM game_spell LIMIT 2")
	if err != nil {
		t.Fatal(err)
	}
	chosen := []CreatureSpell{}
	if err := spells.Each(func(r *stdb.Row) error {
		chosen = append(chosen, CreatureSpell{Spell: int(r.Uint("spell_id"))})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(chosen) != 2 {
		t.Fatal("the disposable realm needs two fixture spells")
	}
	if _, err := SaveLyraCreatureSpells(realm, entry, chosen); err != nil {
		t.Fatal(err)
	}
	read, err := ReadLyraCreatureSpells(realm, entry)
	if err != nil || !reflect.DeepEqual(read.Spells, chosen) || read.Unavailable != "" {
		t.Fatalf("spells: %+v %v", read, err)
	}
	copyEntry, err := CreateLyraCreature(realm, entry, "Meld live copy")
	if err != nil {
		t.Fatal(err)
	}
	copied, err := ReadLyraCreatureSpells(realm, copyEntry)
	if err != nil || !reflect.DeepEqual(copied.Spells, chosen) {
		t.Fatalf("copied spells: %+v %v", copied, err)
	}
	copiedTemplate, err := ReadLyraCreature(realm, copyEntry)
	if err != nil || copiedTemplate.Values["display_id2"] != "200" {
		t.Fatalf("copied displays: %+v %v", copiedTemplate, err)
	}
	if _, err := SaveLyraCreatureSpells(realm, entry, nil); err != nil {
		t.Fatal(err)
	}
	read, err = ReadLyraCreatureSpells(realm, entry)
	if err != nil || len(read.Spells) != 0 {
		t.Fatalf("clear: %+v %v", read, err)
	}
	t.Logf("created, updated and read creature %d; set displays and spells, then cleared spells", entry)
}

func TestLyraCreatureDisplayUsesAndClientScale(t *testing.T) {
	realm, state := newCreatureRealmTest(t)
	data := t.TempDir()
	displays := testDBC(t, 9, []any{100, 1, 0, 0, math.Float32bits(2), 0, "Skin"}, []any{200, 1, 0, 0, math.Float32bits(1), 0, "Skin"})
	models := testDBC(t, 3, []any{1, 0, `Creature\Test\Test.mdx`})
	objects := testDBC(t, 3)
	files := []mpq.File{{Name: `DBFilesClient\CreatureDisplayInfo.dbc`, Data: displays.data}, {Name: `DBFilesClient\CreatureModelData.dbc`, Data: models.data}, {Name: `DBFilesClient\GameObjectDisplayInfo.dbc`, Data: objects.data}}
	if err := os.WriteFile(filepath.Join(data, "base.MPQ"), mpq.Write(files), 0o644); err != nil {
		t.Fatal(err)
	}
	templates, err := ListLyraCreatureTemplates(realm, data)
	if err != nil || len(templates) != 1 || templates[0].Scale != 1.25 || templates[0].DisplayScale != 1 {
		t.Fatalf("absolute scale: %+v %v", templates, err)
	}
	state.displays = append(state.displays, state.displays[0])
	list, err := ListLyraDisplays(realm, data)
	if err != nil || len(list) != 2 || list[0].Uses != 1 || list[1].Uses != 1 || list[0].Names[0] != "Forest keeper" || list[0].Type != 7 {
		t.Fatalf("uses: %+v %v", list, err)
	}
	if _, err := SaveLyraCreature(realm, data, 5200500, map[string]string{"display_scale1": "0"}); err != nil {
		t.Fatal(err)
	}
	if state.fields.Scale != 2 {
		t.Fatalf("client scale: %v", state.fields.Scale)
	}
}
