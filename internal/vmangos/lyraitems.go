package vmangos

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"Meld/internal/lyracore"
	"Meld/internal/stdb"
)

type lyraItemCreate struct {
	Fields lyraItemFields `json:"fields"`
}
type lyraItemUpdate struct {
	Entry  uint32         `json:"entry"`
	Fields lyraItemFields `json:"fields"`
}
type lyraVendorOffer struct {
	ItemEntry uint32 `json:"item_entry"`
	MaxCount  uint32 `json:"max_count"`
}
type lyraVendorOffers struct {
	CreatureEntry uint32            `json:"creature_entry"`
	Offers        []lyraVendorOffer `json:"offers"`
}
type lyraItemEdit struct {
	Create *lyraItemCreate   `json:"createItem,omitempty"`
	Update *lyraItemUpdate   `json:"updateItem,omitempty"`
	Vendor *lyraVendorOffers `json:"setVendorOffers,omitempty"`
}

func readLyraItems(realm *lyracore.Realm, where string) ([]lyraItemRow, error) {
	table, err := realm.DB.Query(realm.Database, "SELECT * FROM game_item_template"+where)
	if err != nil {
		return nil, err
	}
	return lyraItemRows(table)
}

func readLyraItem(realm *lyracore.Realm, entry int) (lyraItemFields, error) {
	rows, err := readLyraItems(realm, fmt.Sprintf(" WHERE entry = %d", entry))
	if err != nil {
		return lyraItemFields{}, err
	}
	if len(rows) != 1 {
		return lyraItemFields{}, fmt.Errorf("the realm has no item %d", entry)
	}
	return rows[0].Fields, nil
}

func ReadLyraItemTemplate(realm *lyracore.Realm, entry int) (TemplateRecord, error) {
	f, err := readLyraItem(realm, entry)
	if err != nil {
		return TemplateRecord{}, err
	}
	return lyraItemRecord(entry, f), nil
}

func ReadLyraItem(realm *lyracore.Realm, entry int) (map[string]string, error) {
	r, err := ReadLyraItemTemplate(realm, entry)
	if err != nil {
		return nil, err
	}
	r.Values["entry"] = strconv.Itoa(entry)
	return r.Values, nil
}

func applyLyraItemEdits(realm *lyracore.Realm, edits []lyraItemEdit, res Result) (Result, error) {
	if len(edits) > 64 {
		return res, refuse("", "a batch holds at most 64 item edits")
	}
	offers := 0
	for _, e := range edits {
		if e.Vendor != nil {
			if len(e.Vendor.Offers) > 128 {
				return res, refuse("", "a vendor holds at most 128 offers")
			}
			offers += len(e.Vendor.Offers)
		}
	}
	if offers > 512 {
		return res, refuse("", "a batch holds at most 512 vendor offers")
	}
	res.Live = true
	if len(edits) == 0 {
		return res, nil
	}
	b, err := json.Marshal(edits)
	if err != nil {
		return res, err
	}
	res.SQL = string(b)
	if err := realm.DB.Call(realm.Database, "operator_apply_item_edits", edits); err != nil {
		var response *stdb.HTTPError
		if errors.As(err, &response) && response.StatusCode == stdb.ReducerFailedStatus {
			return res, refuse("", "%s", err.Error())
		}
		return res, err
	}
	forgetWeapons()
	return res, nil
}

func SaveLyraItem(realm *lyracore.Realm, entry int, values map[string]string) (Result, error) {
	current, err := readLyraItem(realm, entry)
	if err != nil {
		return Result{}, err
	}
	fields, skipped, err := lyraItemChanges(current, values)
	res := Result{Live: true, Skipped: skipped}
	if err != nil || fields == current {
		return res, err
	}
	res.Changed = 1
	return applyLyraItemEdits(realm, []lyraItemEdit{{Update: &lyraItemUpdate{Entry: uint32(entry), Fields: fields}}}, res)
}

func createLyraItem(realm *lyracore.Realm, fields lyraItemFields) (int, error) {
	if _, err := applyLyraItemEdits(realm, []lyraItemEdit{{Create: &lyraItemCreate{Fields: fields}}}, Result{Added: 1}); err != nil {
		return 0, err
	}
	table, err := realm.DB.Query(realm.Database, "SELECT entry FROM game_item_template WHERE entry >= 5200500 AND entry <= 5999999")
	if err != nil {
		return 0, fmt.Errorf("item created, but its entry could not be read: %w", err)
	}
	var entry uint32
	if err := table.Each(func(r *stdb.Row) error { entry = max(entry, uint32(r.Uint("entry"))); return nil }); err != nil {
		return 0, err
	}
	if entry == 0 {
		return 0, fmt.Errorf("item created, but the Operator Template Range is empty")
	}
	return int(entry), nil
}

func CreateLyraItem(realm *lyracore.Realm, base int, name string) (int, error) {
	fields := newLyraItem(name)
	if base != 0 {
		var err error
		fields, err = readLyraItem(realm, base)
		if err != nil {
			return 0, err
		}
		fields.Name = name
	}
	return createLyraItem(realm, fields)
}

func ListLyraItemKinds(realm *lyracore.Realm) ([]ItemKind, error) {
	table, err := realm.DB.Query(realm.Database, "SELECT class, subclass, inventory_type FROM game_item_template")
	if err != nil {
		return nil, err
	}
	counts := map[ItemKind]int{}
	err = table.Each(func(r *stdb.Row) error {
		counts[ItemKind{Class: int(r.Uint("class")), Subclass: int(r.Uint("subclass")), InvType: int(r.Uint("inventory_type"))}]++
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := []ItemKind{}
	for k, n := range counts {
		if n >= minKindItems {
			k.Count = n
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Class != b.Class {
			return a.Class < b.Class
		}
		if a.Subclass != b.Subclass {
			return a.Subclass < b.Subclass
		}
		return a.InvType < b.InvType
	})
	return out, nil
}

func CreateLyraItemOfKind(realm *lyracore.Realm, k ItemKind, name string) (int, error) {
	rows, err := readLyraItems(realm, fmt.Sprintf(" WHERE class = %d AND subclass = %d AND inventory_type = %d", k.Class, k.Subclass, k.InvType))
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("the realm has no item of that kind to start from")
	}
	fields := newLyraItem(name)
	var cols []string
	for _, c := range append(append([]string{}, kindBasics...), "delay", "max_durability") {
		if _, ok := lyraItemRecord(0, fields).Values[c]; ok {
			cols = append(cols, c)
		}
	}
	var samples []batchRow
	for _, row := range rows {
		r := lyraItemRecord(int(row.Entry), row.Fields)
		sample := make(batchRow, len(cols))
		for i, c := range cols {
			sample[i] = r.Values[c]
		}
		samples = append(samples, sample)
	}
	values := usualItemValues(cols, samples)
	values["class"], values["subclass"], values["inventory_type"] = strconv.Itoa(k.Class), strconv.Itoa(k.Subclass), strconv.Itoa(k.InvType)
	fields, _, err = lyraItemChanges(fields, values)
	if err != nil {
		return 0, err
	}
	return createLyraItem(realm, fields)
}

func SearchLyraItems(realm *lyracore.Realm, text, order string, desc bool, limit int) ([]ItemRef, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return []ItemRef{}, nil
	}
	table, err := realm.DB.Query(realm.Database, "SELECT entry, name, quality, display_id, item_level, required_level FROM game_item_template")
	if err != nil {
		return nil, err
	}
	out := []ItemRef{}
	err = table.Each(func(r *stdb.Row) error {
		entry, name := int(r.Uint("entry")), r.String("name")
		if strings.Contains(strings.ToLower(name), text) || strconv.Itoa(entry) == text {
			out = append(out, ItemRef{Entry: entry, Name: name, Quality: int(r.Uint("quality")), DisplayID: int(r.Uint("display_id")), ItemLevel: int(r.Uint("item_level")), RequiredLevel: int(r.Uint("required_level"))})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	compare := func(a, b ItemRef) int {
		switch order {
		case "entry":
			return a.Entry - b.Entry
		case "quality":
			return a.Quality - b.Quality
		case "itemLevel":
			return a.ItemLevel - b.ItemLevel
		case "requiredLevel":
			return a.RequiredLevel - b.RequiredLevel
		default:
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		c := compare(a, b)
		if c != 0 {
			if desc {
				return c > 0
			}
			return c < 0
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Entry < b.Entry
	})
	return out[:min(len(out), max(1, limit))], nil
}

func ReadLyraVendor(realm *lyracore.Realm, entry int) (Vendor, error) {
	t, err := realm.DB.Query(realm.Database, fmt.Sprintf("SELECT * FROM game_npc_vendor WHERE creature_entry = %d", entry))
	if err != nil {
		return Vendor{}, err
	}
	type offer struct {
		slot uint64
		id   uint64
		item VendorItem
	}
	var offers []offer
	ids := map[uint64]bool{}
	err = t.Each(func(r *stdb.Row) error {
		item := r.Uint("item_entry")
		ids[item] = true
		offers = append(offers, offer{slot: r.Uint("slot"), id: r.Uint("id"), item: VendorItem{Item: int(item), MaxCount: int(r.Uint("max_count"))}})
		return nil
	})
	if err != nil {
		return Vendor{}, err
	}
	sort.Slice(offers, func(i, j int) bool {
		if offers[i].slot == offers[j].slot {
			return offers[i].id < offers[j].id
		}
		return offers[i].slot < offers[j].slot
	})
	names := map[int]ItemRef{}
	err = lyraLookup(realm.DB, realm.Database, "game_item_template", "entry", ids, func(r *stdb.Row) {
		id := int(r.Uint("entry"))
		names[id] = ItemRef{Name: r.String("name"), Quality: int(r.Uint("quality")), DisplayID: int(r.Uint("display_id"))}
	})
	if err != nil {
		return Vendor{}, err
	}
	v := Vendor{Items: []VendorItem{}, Template: []VendorItem{}}
	for _, o := range offers {
		it := o.item
		n := names[it.Item]
		it.Name, it.Quality, it.DisplayID = n.Name, n.Quality, n.DisplayID
		v.Items = append(v.Items, it)
	}
	return v, nil
}

func lyraVendorEdit(entry int, items []VendorItem) (lyraItemEdit, []string, error) {
	if entry <= 0 || uint64(entry) > math.MaxUint32 {
		return lyraItemEdit{}, nil, refuse("", "creature entry must fit a positive u32")
	}
	v := &lyraVendorOffers{CreatureEntry: uint32(entry), Offers: []lyraVendorOffer{}}
	var skipped []string
	for _, it := range items {
		if it.Item <= 0 || uint64(it.Item) > math.MaxUint32 || it.MaxCount < 0 || uint64(it.MaxCount) > math.MaxUint32 {
			return lyraItemEdit{}, nil, refuse("", "item entry and stock must fit u32, with a positive item entry")
		}
		v.Offers = append(v.Offers, lyraVendorOffer{ItemEntry: uint32(it.Item), MaxCount: uint32(it.MaxCount)})
		for _, extra := range []struct {
			name  string
			value int
		}{{"incrtime", it.IncrTime}, {"itemflags", it.ItemFlags}, {"condition_id", it.Condition}} {
			if extra.value != 0 {
				skipped = append(skipped, fmt.Sprintf("item %d %s: LyraCore has no equivalent", it.Item, extra.name))
			}
		}
	}
	return lyraItemEdit{Vendor: v}, skipped, nil
}

func SaveLyraVendor(realm *lyracore.Realm, entry int, items []VendorItem) (Result, error) {
	e, skipped, err := lyraVendorEdit(entry, items)
	if err != nil {
		return Result{}, err
	}
	return applyLyraItemEdits(realm, []lyraItemEdit{e}, Result{Changed: 1, Skipped: skipped})
}

func LyraEquipBonuses(realm *lyracore.Realm, dataDir string) ([]EquipBonusKind, error) {
	var columns []string
	for i := 1; i <= 5; i++ {
		columns = append(columns, fmt.Sprintf("spellid_%d", i), fmt.Sprintf("spelltrigger_%d", i))
	}
	table, err := realm.DB.Query(realm.Database, "SELECT "+strings.Join(columns, ", ")+" FROM game_item_template")
	if err != nil {
		return nil, err
	}
	used := map[int]bool{}
	err = table.Each(func(r *stdb.Row) error {
		for i := 0; i < len(columns); i += 2 {
			if r.Uint(columns[i+1]) == 1 {
				used[int(r.Uint(columns[i]))] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	spellData.Lock()
	defer spellData.Unlock()
	if err := loadSpellData(dataDir); err != nil {
		return nil, err
	}
	return equipBonuses(spellData.spells, used), nil
}

func ListLyraItemDisplays(realm *lyracore.Realm, dataDir string) ([]ItemDisplayEntry, error) {
	table, err := realm.DB.Query(realm.Database, "SELECT display_id, inventory_type, name, class, subclass FROM game_item_template WHERE display_id <> 0")
	if err != nil {
		return nil, err
	}
	b, err := readClientFile(dataDir, ItemDisplayPath)
	if err != nil {
		return nil, err
	}
	t, err := parseDBC(b)
	if err != nil {
		return nil, err
	}
	if t.fields < itemDisplayFields-1 {
		return nil, fmt.Errorf("ItemDisplayInfo.dbc has too few fields")
	}
	var rows []batchRow
	err = table.Each(func(r *stdb.Row) error {
		rows = append(rows, batchRow{strconv.FormatUint(r.Uint("display_id"), 10), strconv.FormatUint(r.Uint("inventory_type"), 10), r.String("name"), strconv.FormatUint(r.Uint("class"), 10), strconv.FormatUint(r.Uint("subclass"), 10)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][2] < rows[j][2] })
	return listItemDisplays(t, rows), nil
}

// ReadLyraVendorCreature provides the read-only creature summary needed to open its Shop tab.
func ReadLyraVendorCreature(realm *lyracore.Realm, entry int) (TemplateRecord, error) {
	t, err := realm.DB.Query(realm.Database, fmt.Sprintf("SELECT * FROM game_creature_template WHERE entry = %d", entry))
	if err != nil {
		return TemplateRecord{}, err
	}
	if len(t.Rows) != 1 {
		return TemplateRecord{}, fmt.Errorf("the realm has no creature %d", entry)
	}
	record := TemplateRecord{Entry: entry, ReadOnly: true, Columns: []TemplateColumn{}, Values: map[string]string{}}
	err = t.Each(func(r *stdb.Row) error {
		record.Values["name"], record.Values["subname"] = r.String("name"), r.String("subname")
		for _, pair := range [][2]string{{"npc_flags", "npc_flags"}, {"level_min", "level"}, {"level_max", "max_level"}, {"rank", "rank"}, {"display_id1", "display_id"}, {"faction", "faction_template"}} {
			record.Values[pair[0]] = strconv.FormatUint(r.Uint(pair[1]), 10)
		}
		return nil
	})
	return record, err
}
