package vmangos

import (
	"encoding/json"
	"io"
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

func lyraTestItem() lyraItemFields {
	return lyraItemFields{Class: 2, Subclass: 7, Name: "Meld blade", DisplayID: 1542, Quality: 3,
		InventoryType: 21, ItemLevel: 60, RequiredLevel: 55, MaxDurability: 90, BuyPrice: 20000,
		SellPrice: 4000, MaxStack: 1, DamageMin: 35.5, DamageMax: 72.25, DelayMS: 2400,
		StatStrength: 12, StatAgility: 8, StatStamina: 10, StatIntellect: -2, StatSpirit: 3,
		StatCrit: 125, StatHit: 75, StatArmor: 150, BlockValue: 7, RestoresPower: true,
		SpellID1: 123, SpellTrigger1: 1, SpellID2: 456, SpellTrigger2: 2, SpellID3: 789,
		SpellTrigger3: 0, SpellID4: 1000, SpellTrigger4: 1, SpellID5: 1001, SpellTrigger5: 2,
		ContainerSlots: 4, Sheath: 3, Bonding: 2, HolyRes: 1, FireRes: 2, NatureRes: 3,
		FrostRes: 4, ShadowRes: 5, ArcaneRes: 6, RequiredSkill: 43, RequiredSkillRank: 250,
		RequiredReputationFaction: 72, RequiredReputationRank: 4, MaxCount: 1, ItemFlags: 64,
		PageText: 22, StartQuest: 33, BagFamily: 4, BuyCount: 1, FoodType: 0,
		AllowedClass: 0x5df, AllowedRace: 0xff, RandomProperty: 88}
}

func itemTableForTest(t *testing.T, entry uint32, f lyraItemFields) stdb.Table {
	t.Helper()
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	fields["entry"], _ = json.Marshal(entry)
	var table stdb.Table
	for k := range fields {
		table.Columns = append(table.Columns, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(table.Columns)))
	row := make([]json.RawMessage, len(table.Columns))
	for i, k := range table.Columns {
		row[i] = fields[k]
	}
	table.Rows = [][]json.RawMessage{row}
	return table
}

func TestLyraItemMappingRoundTrip(t *testing.T) {
	want := lyraTestItem()
	table := itemTableForTest(t, 25, want)
	rows, err := lyraItemRows(table)
	if err != nil || len(rows) != 1 || rows[0].Fields != want {
		t.Fatalf("read: %+v, %v", rows, err)
	}
	r := lyraItemRecord(25, rows[0].Fields)
	for k, v := range map[string]string{"stat_type1": "3", "stat_value1": "8", "stat_type2": "4", "stat_value2": "12", "delay": "2400", "dmg_min1": "35.5", "armor": "150", "block": "7", "stackable": "1", "flags": "64", "allowable_class": "1503"} {
		if r.Values[k] != v {
			t.Errorf("%s = %q, want %q", k, r.Values[k], v)
		}
	}
	got, skipped, err := lyraItemChanges(want, r.Values)
	if err != nil || len(skipped) != 0 || got != want {
		t.Fatalf("round trip: %+v, %v, %v", got, skipped, err)
	}
	got, skipped, err = lyraItemChanges(want, map[string]string{"stat_value2": "15", "stat_type6": "4", "stat_value6": "2", "name": "Changed", "delay": "1900"})
	want.StatStrength, want.Name, want.DelayMS = 17, "Changed", 1900
	if err != nil || len(skipped) != 0 || got != want {
		t.Fatalf("changed: %+v, %v, %v", got, skipped, err)
	}
	for _, c := range r.Columns {
		if c.Nullable {
			t.Errorf("nullable %s", c.Name)
		}
		switch c.Name {
		case "quality":
			if c.Kind != "uint" || c.Max != 255 {
				t.Errorf("quality: %+v", c)
			}
		case "armor":
			if c.Kind != "int" || c.Min != -2147483648 || c.Max != 2147483647 {
				t.Errorf("armor: %+v", c)
			}
		case "name":
			if c.Kind != "text" || c.MaxLen != 255 {
				t.Errorf("name: %+v", c)
			}
		case "dmg_min1":
			if c.Kind != "float" {
				t.Errorf("damage: %+v", c)
			}
		case "dmg_type1", "stat_crit", "stat_hit", "restores_power", "spellcharges_1":
			t.Errorf("unsupported editable column %s", c.Name)
		}
	}
}

func TestLyraItemUnsupportedAndInvalidValues(t *testing.T) {
	want := lyraTestItem()
	got, skipped, err := lyraItemChanges(want, map[string]string{"name": "Changed", "stat_type6": "1", "stat_value6": "20", "stat_value2": "99", "dmg_type1": "2", "spellcharges_1": "4"})
	want.Name = "Changed"
	if err != nil || len(skipped) != 3 || got != want {
		t.Fatalf("unsupported changes: %+v, %v, %v", got, skipped, err)
	}
	for _, values := range []map[string]string{{"quality": "256"}, {"armor": "2147483648"}, {"dmg_min1": "NaN"}, {"dmg_max1": "1e100"}, {"name": " "}, {"name": strings.Repeat("é", 128)}, {"stat_value1": "2147483647", "stat_type6": "3", "stat_value6": "1"}} {
		if _, _, err := lyraItemChanges(want, values); Refused(err) == nil {
			t.Errorf("not refused: %v, %v", values, err)
		}
	}
}

func TestLyraItemDefaultsAndMasks(t *testing.T) {
	f := newLyraItem("Junk")
	if f.Class != 15 || f.Quality != 1 || f.MaxStack != 1 || f.BuyCount != 1 || f.AllowedClass != 1503 || f.AllowedRace != 255 || f.StatCrit != 0 || f.StatHit != 0 || f.RestoresPower {
		t.Fatalf("defaults: %+v", f)
	}
	f.AllowedClass, f.AllowedRace = 1, 2
	got, _, err := lyraItemChanges(f, map[string]string{"allowable_class": "-1", "allowable_race": "-1"})
	if err != nil || got.AllowedClass != 1503 || got.AllowedRace != 255 {
		t.Fatalf("masks: %+v %v", got, err)
	}
	if _, _, err := lyraItemChanges(f, map[string]string{"allowable_class": "-2"}); err == nil {
		t.Fatal("accepted negative mask")
	}
}

func writeLyraTable(t *testing.T, w http.ResponseWriter, table stdb.Table) {
	t.Helper()
	elements := []any{}
	for _, c := range table.Columns {
		elements = append(elements, map[string]any{"name": map[string]string{"some": c}})
	}
	if err := json.NewEncoder(w).Encode([]any{map[string]any{"schema": map[string]any{"elements": elements}, "rows": table.Rows}}); err != nil {
		t.Error(err)
	}
}

func TestLyraItemReducerBodiesAndFreshRead(t *testing.T) {
	current := lyraTestItem()
	current.StatCrit = 333
	var calls [][]lyraItemEdit
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/sql") {
			if !strings.HasPrefix(string(b), "SELECT ") {
				t.Errorf("SQL write: %s", b)
				w.WriteHeader(500)
				return
			}
			writeLyraTable(t, w, itemTableForTest(t, 5200500, current))
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/call/operator_apply_item_edits") {
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
		}
		var args [][]lyraItemEdit
		if err := json.Unmarshal(b, &args); err != nil || len(args) != 1 {
			t.Errorf("body %s: %v", b, err)
			return
		}
		calls = append(calls, args[0])
		var raw [][]map[string]json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Error(err)
			return
		}
		for _, edit := range raw[0] {
			if len(edit) != 1 {
				t.Errorf("variant: %s", b)
			}
			for k, payload := range edit {
				if k == "setVendorOffers" {
					continue
				}
				var p struct {
					Fields map[string]any `json:"fields"`
				}
				if err := json.Unmarshal(payload, &p); err != nil {
					t.Error(err)
				}
				if len(p.Fields) != 58 {
					t.Errorf("incomplete fields: %d", len(p.Fields))
				}
			}
		}
	}))
	defer server.Close()
	realm := &lyracore.Realm{DB: &stdb.Client{URL: server.URL}, Database: "test"}
	entry, err := CreateLyraItem(realm, 0, "New")
	if err != nil || entry != 5200500 {
		t.Fatalf("create: %d %v", entry, err)
	}
	res, err := SaveLyraItem(realm, 5200500, map[string]string{"name": "Renamed"})
	if err != nil || !res.Live || res.Changed != 1 || !strings.Contains(res.SQL, `"updateItem"`) {
		t.Fatalf("save: %+v %v", res, err)
	}
	res, err = SaveLyraVendor(realm, 620, []VendorItem{{Item: 25, MaxCount: 3, IncrTime: 60}, {Item: 5200500, ItemFlags: 1, Condition: 2}})
	if err != nil || len(res.Skipped) != 3 || !res.Live {
		t.Fatalf("vendor: %+v %v", res, err)
	}
	if _, err := SaveLyraVendor(realm, 620, nil); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Fatalf("calls: %+v", calls)
	}
	if calls[0][0].Create.Fields != newLyraItem("New") {
		t.Errorf("create body: %+v", calls[0])
	}
	want := current
	want.Name = "Renamed"
	if calls[1][0].Update.Entry != 5200500 || calls[1][0].Update.Fields != want {
		t.Errorf("update lost fresh fields: %+v", calls[1])
	}
	if !reflect.DeepEqual(calls[2][0].Vendor, &lyraVendorOffers{CreatureEntry: 620, Offers: []lyraVendorOffer{{ItemEntry: 25, MaxCount: 3}, {ItemEntry: 5200500}}}) {
		t.Errorf("vendor body: %+v", calls[2])
	}
	if calls[3][0].Vendor.Offers == nil || len(calls[3][0].Vendor.Offers) != 0 {
		t.Fatal("empty vendor offers must be []")
	}
}

func TestLyraItemLimitsAndRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "item edit 0: item 7000000 belongs to the Package Item Range", stdb.ReducerFailedStatus)
	}))
	defer server.Close()
	realm := &lyracore.Realm{DB: &stdb.Client{URL: server.URL}, Database: "test"}
	_, err := applyLyraItemEdits(realm, []lyraItemEdit{{Update: &lyraItemUpdate{Entry: 7000000, Fields: newLyraItem("Refused")}}}, Result{})
	if Refused(err) == nil || !strings.Contains(err.Error(), "Package Item Range") {
		t.Fatalf("refusal: %v", err)
	}
	for _, edits := range [][]lyraItemEdit{make([]lyraItemEdit, 65), {{Vendor: &lyraVendorOffers{Offers: make([]lyraVendorOffer, 129)}}}, {{Vendor: &lyraVendorOffers{Offers: make([]lyraVendorOffer, 128)}}, {Vendor: &lyraVendorOffers{Offers: make([]lyraVendorOffer, 128)}}, {Vendor: &lyraVendorOffers{Offers: make([]lyraVendorOffer, 128)}}, {Vendor: &lyraVendorOffers{Offers: make([]lyraVendorOffer, 128)}}, {Vendor: &lyraVendorOffers{Offers: make([]lyraVendorOffer, 1)}}}} {
		_, err := applyLyraItemEdits(nil, edits, Result{})
		if Refused(err) == nil {
			t.Fatalf("limits: %v", err)
		}
	}
}

func TestLyraItemReadsCopiesAndUsualValues(t *testing.T) {
	base := lyraTestItem()
	second, third := base, base
	second.Name, third.Name = "Other blade", "Last blade"
	second.DelayMS, third.DelayMS = 1800, 3000
	second.DisplayID = 99
	third.RequiredLevel = 1
	items := itemTableForTest(t, 25, base)
	items.Rows = append(items.Rows, itemTableForTest(t, 26, second).Rows[0], itemTableForTest(t, 27, third).Rows[0])
	var created []lyraItemFields
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/call/operator_apply_item_edits") {
			var args [][]lyraItemEdit
			if err := json.Unmarshal(b, &args); err != nil {
				t.Error(err)
				return
			}
			created = append(created, args[0][0].Create.Fields)
			return
		}
		q := string(b)
		if !strings.HasPrefix(q, "SELECT ") || strings.Contains(q, " IN ") || strings.Contains(q, " JOIN ") {
			t.Errorf("query: %s", q)
		}
		switch {
		case strings.Contains(q, "game_npc_vendor"):
			writeLyraTable(t, w, stdb.Table{Columns: []string{"id", "creature_entry", "item_entry", "slot", "max_count"}, Rows: [][]json.RawMessage{{json.RawMessage("9"), json.RawMessage("620"), json.RawMessage("26"), json.RawMessage("1"), json.RawMessage("3")}, {json.RawMessage("8"), json.RawMessage("620"), json.RawMessage("25"), json.RawMessage("0"), json.RawMessage("0")}}})
		case strings.Contains(q, "entry >= 5200500"):
			writeLyraTable(t, w, itemTableForTest(t, 5200500, base))
		case strings.HasSuffix(q, " WHERE entry = 25"):
			writeLyraTable(t, w, itemTableForTest(t, 25, base))
		default:
			writeLyraTable(t, w, items)
		}
	}))
	defer server.Close()
	realm := &lyracore.Realm{DB: &stdb.Client{URL: server.URL}, Database: "test"}
	refs, err := SearchLyraItems(realm, "blade", "requiredLevel", true, 2)
	if err != nil || len(refs) != 2 || refs[0].Entry != 25 || refs[1].Entry != 26 {
		t.Fatalf("sorted search: %+v %v", refs, err)
	}
	refs, err = SearchLyraItems(realm, "27", "name", false, 100)
	if err != nil || len(refs) != 1 || refs[0].Entry != 27 {
		t.Fatalf("entry search: %+v %v", refs, err)
	}
	kinds, err := ListLyraItemKinds(realm)
	if err != nil || !reflect.DeepEqual(kinds, []ItemKind{{Class: 2, Subclass: 7, InvType: 21, Count: 3}}) {
		t.Fatalf("kinds: %+v %v", kinds, err)
	}
	vendor, err := ReadLyraVendor(realm, 620)
	if err != nil || len(vendor.Items) != 2 || vendor.Items[0].Item != 25 || vendor.Items[1].Item != 26 || vendor.Items[1].MaxCount != 3 || vendor.Items[1].Name != "Other blade" {
		t.Fatalf("vendor: %+v %v", vendor, err)
	}
	if _, err := CreateLyraItem(realm, 25, "Copy"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateLyraItemOfKind(realm, ItemKind{Class: 2, Subclass: 7, InvType: 21}, "Usual"); err != nil {
		t.Fatal(err)
	}
	want := base
	want.Name = "Copy"
	if len(created) != 2 || created[0] != want {
		t.Fatalf("copy: %+v", created)
	}
	usual := created[1]
	if usual.Name != "Usual" || usual.Class != 2 || usual.Subclass != 7 || usual.InventoryType != 21 || usual.DisplayID != 1542 || usual.DelayMS != 2400 || usual.MaxDurability != 90 || usual.StatStrength != 0 || usual.DamageMax != 0 || usual.SpellID1 != 0 || usual.SellPrice != 0 {
		t.Fatalf("usual: %+v", usual)
	}
}

func TestLyraItemTransportFailureIsNotARefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	realm := &lyracore.Realm{DB: &stdb.Client{URL: server.URL}, Database: "test"}
	_, err := applyLyraItemEdits(realm, []lyraItemEdit{{Create: &lyraItemCreate{Fields: newLyraItem("New")}}}, Result{})
	if err == nil || Refused(err) != nil {
		t.Fatalf("transport failure: %v", err)
	}
}

func TestLyraItemDisplaysAndEquipBonusesUseRealm(t *testing.T) {
	data := t.TempDir()
	spells := testDBC(t, spellFDescription+1,
		bonusSpell(10, "Healing", auraModHealingDone, 0, schoolMaskMagic, 22),
		bonusSpell(11, "Healing again", auraModHealingDone, 0, schoolMaskMagic, 22))
	files := []mpq.File{{Name: ItemDisplayPath, Data: itemDisplayDBC(t).data}, {Name: SpellPath, Data: spells.data}}
	if err := os.WriteFile(filepath.Join(data, "base.MPQ"), mpq.Write(files), 0o644); err != nil {
		t.Fatal(err)
	}
	f := lyraTestItem()
	f.DisplayID, f.SpellID1 = 7, 11
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.HasPrefix(string(b), "SELECT ") {
			t.Errorf("not a read: %s", b)
		}
		writeLyraTable(t, w, itemTableForTest(t, 25, f))
	}))
	defer server.Close()
	realm := &lyracore.Realm{DB: &stdb.Client{URL: server.URL}, Database: "test"}
	displays, err := ListLyraItemDisplays(realm, data)
	if err != nil || len(displays) != 4 || displays[0].ID != 7 || displays[0].Uses != 1 || displays[0].Names[0] != "Meld blade" {
		t.Fatalf("displays: %+v %v", displays, err)
	}
	bonuses, err := LyraEquipBonuses(realm, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, bonus := range bonuses {
		if bonus.ID == "healing" {
			if !reflect.DeepEqual(bonus.Amounts, []EquipBonusSpell{{Spell: 11, Amount: 22}}) {
				t.Fatalf("realm spell preference: %+v", bonus)
			}
			return
		}
	}
	t.Fatal("no healing bonus")
}

// This test leaves its new item in the throwaway realm because item deletion is not supported.
func TestApplyLyraItemEditsLive(t *testing.T) {
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
	creatures, err := realm.DB.Query(realm.Database, "SELECT entry FROM game_creature_template LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	entry := 0
	if err := creatures.Each(func(r *stdb.Row) error { entry = int(r.Uint("entry")); return nil }); err != nil {
		t.Fatal(err)
	}
	if entry == 0 {
		t.Fatal("seed a creature template in the disposable realm first")
	}
	old, err := ReadLyraVendor(realm, entry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := SaveLyraVendor(realm, entry, old.Items); err != nil {
			t.Error(err)
		}
	}()
	item, err := CreateLyraItem(realm, 0, "Meld live item")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveLyraItem(realm, item, map[string]string{"name": "Meld renamed item", "stat_value2": "12"}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLyraItemTemplate(realm, item)
	if err != nil || got.Values["name"] != "Meld renamed item" || got.Values["stat_value2"] != "12" {
		t.Fatalf("item readback: %+v %v", got, err)
	}
	if _, err := SaveLyraVendor(realm, entry, []VendorItem{{Item: item, MaxCount: 3}}); err != nil {
		t.Fatal(err)
	}
	vendor, err := ReadLyraVendor(realm, entry)
	if err != nil || len(vendor.Items) != 1 || vendor.Items[0].Item != item || vendor.Items[0].MaxCount != 3 {
		t.Fatalf("vendor readback: %+v %v", vendor, err)
	}
}
