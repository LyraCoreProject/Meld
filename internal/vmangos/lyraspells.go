package vmangos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"Meld/internal/lyracore"
	"Meld/internal/mpq"
	"Meld/internal/stdb"
)

// LyraCore's Operator Spell Range: the 1.12 initial spellbook packet carries 16-bit spell ids.
const (
	lyraSpellFloor = 60000
	lyraSpellCeil  = 65535
)

type lyraSpellSourceField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type lyraSpellRequest struct {
	Create   bool                         `json:"create"`
	Source   map[string]string            `json:"source"`
	Trainers []map[string]json.RawMessage `json:"trainers"`
}

func runSpellCompiler(input []byte, args ...string) ([]byte, error) {
	compiler := os.Getenv("MELD_LYRACORE_SPELL_COMPILER")
	if compiler == "" {
		return nil, errors.New("set MELD_LYRACORE_SPELL_COMPILER to LyraCore's native-host/editor-spells.py")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, compiler, args...)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("spell compiler: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func worldSpellSource(entry int) (map[string]string, error) {
	out, err := runSpellCompiler(nil, "--read-spell-source", strconv.Itoa(entry))
	if err != nil {
		return nil, err
	}
	var source map[string]string
	if err := json.Unmarshal(out, &source); err != nil {
		return nil, fmt.Errorf("spell source: %w", err)
	}
	return source, nil
}

func lyraSpellColumns(source map[string]string) []TemplateColumn {
	columns := make([]TemplateColumn, 0, len(source))
	for name := range source {
		if name == "entry" || name == "build" {
			continue
		}
		c := TemplateColumn{Name: name, Kind: "uint", Max: math.MaxUint32}
		switch {
		case spellTextColumns[name] || name == "script_name":
			c.Kind, c.Max = "text", 0
		case name == "speed" || strings.HasPrefix(name, "effectBonusCoefficient") || strings.HasPrefix(name, "effectDicePerLevel") || strings.HasPrefix(name, "effectRealPointsPerLevel") || strings.HasPrefix(name, "effectMultipleValue") || strings.HasPrefix(name, "effectPointsPerComboPoint") || strings.HasPrefix(name, "dmgMultiplier"):
			c.Kind, c.Max = "float", 0
		case name == "powerType" || name == "equippedItemClass" || name == "equippedItemSubClassMask" || name == "equippedItemInventoryTypeMask" || name == "stanceBarOrder" || strings.HasPrefix(name, "effectBasePoints") || strings.HasPrefix(name, "effectMiscValue") || strings.HasPrefix(name, "effectDieSides") || strings.HasPrefix(name, "reagent") && !strings.HasPrefix(name, "reagentCount"):
			c.Kind, c.Min, c.Max = "int", math.MinInt32, math.MaxInt32
		case name == "spellFamilyFlags" || strings.HasPrefix(name, "effectItemType"):
			c.Max = math.MaxInt64
		}
		columns = append(columns, c)
	}
	sort.Slice(columns, func(i, j int) bool { return columns[i].Name < columns[j].Name })
	return columns
}

func ReadLyraSpell(realm *lyracore.Realm, dataDir string, entry int) (SpellRecord, error) {
	if entry <= 0 || uint64(entry) > math.MaxUint32 {
		return SpellRecord{}, refuse("entry", "invalid spell id")
	}
	present, err := realm.DB.Query(realm.Database, fmt.Sprintf("SELECT spell_id FROM game_spell WHERE spell_id = %d", entry))
	if err != nil {
		return SpellRecord{}, err
	}
	if len(present.Rows) != 1 {
		return SpellRecord{}, fmt.Errorf("the Realm has no spell %d", entry)
	}
	table, err := realm.DB.Query(realm.Database, fmt.Sprintf("SELECT fields FROM game_operator_spell_source WHERE spell_id = %d", entry))
	if err != nil {
		return SpellRecord{}, err
	}
	source := map[string]string{}
	if len(table.Rows) == 1 {
		column := table.Col("fields")
		if column < 0 || column >= len(table.Rows[0]) {
			return SpellRecord{}, errors.New("the Realm returned no spell source fields")
		}
		var fields []lyraSpellSourceField
		if err := json.Unmarshal(table.Rows[0][column], &fields); err != nil {
			return SpellRecord{}, err
		}
		for _, field := range fields {
			source[field.Name] = field.Value
		}
	} else {
		source, err = worldSpellSource(entry)
		if err != nil {
			return SpellRecord{}, err
		}
	}
	build, err := strconv.Atoi(source["build"])
	if err != nil {
		return SpellRecord{}, fmt.Errorf("spell %d source has no build: %w", entry, err)
	}
	rec := SpellRecord{Entry: entry, Build: build, Columns: lyraSpellColumns(source), Values: source}
	spellData.Lock()
	defer spellData.Unlock()
	if err := loadSpellData(dataDir); err != nil {
		return SpellRecord{}, err
	}
	_, rec.InClient = spellData.spells.row(uint32(entry))
	icon, _ := strconv.ParseUint(source["spellIconId"], 10, 32)
	rec.Icon = spellData.icons[uint32(icon)]
	return rec, nil
}

func lyraSpellTrainers(realm *lyracore.Realm, entry int) ([]map[string]json.RawMessage, error) {
	table, err := realm.DB.Query(realm.Database, fmt.Sprintf("SELECT * FROM game_trainer_spell WHERE spell_id = %d", entry))
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]json.RawMessage, 0, len(table.Rows))
	for _, values := range table.Rows {
		row := make(map[string]json.RawMessage, len(table.Columns))
		for i, name := range table.Columns {
			row[name] = values[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func compileLyraSpell(dataDir string, request lyraSpellRequest) (json.RawMessage, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(cache, "Meld", "spell-compiler")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(root, "dbc-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	set, err := mpq.OpenDataDir(dataDir)
	if err != nil {
		return nil, err
	}
	defer set.Close()
	for _, name := range []string{"SpellCastTimes.dbc", "SpellRange.dbc", "SpellDuration.dbc", "SpellRadius.dbc"} {
		b, err := readOverlay(set, dataDir, `DBFilesClient\`+name)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			return nil, err
		}
	}
	b, err := json.Marshal([]lyraSpellRequest{request})
	if err != nil {
		return nil, err
	}
	out, err := runSpellCompiler(b, "--compile-spell-edits", "-", "--dbc", dir)
	if err != nil {
		return nil, err
	}
	var edits []json.RawMessage
	if err := json.Unmarshal(out, &edits); err != nil || len(edits) != 1 {
		return nil, errors.New("spell compiler did not return one edit")
	}
	return json.RawMessage(out), nil
}

func applyLyraSpell(realm *lyracore.Realm, edits json.RawMessage) error {
	if err := realm.DB.Call(realm.Database, "operator_apply_spell_edits", edits); err != nil {
		var response *stdb.HTTPError
		if errors.As(err, &response) && response.StatusCode == stdb.ReducerFailedStatus {
			return refuse("", "%s", err.Error())
		}
		return err
	}
	return nil
}

func SaveLyraSpell(realm *lyracore.Realm, dataDir string, entry, build int, values map[string]string, icon string, visual *VisualDesign) (SpellSave, error) {
	record, err := ReadLyraSpell(realm, dataDir, entry)
	if err != nil {
		return SpellSave{}, err
	}
	if build != record.Build {
		return SpellSave{}, refuse("build", "the spell source build changed; read it again")
	}
	plan, err := planSpellSaveRow(dataDir, entry, record.Columns, record.Values, values, icon, visual)
	if err != nil {
		return SpellSave{}, err
	}
	trainers, err := lyraSpellTrainers(realm, entry)
	if err != nil {
		return SpellSave{}, err
	}
	edits, err := compileLyraSpell(dataDir, lyraSpellRequest{Source: plan.Values, Trainers: trainers})
	if err != nil {
		return SpellSave{}, err
	}
	if err := applyLyraSpell(realm, edits); err != nil {
		return SpellSave{}, err
	}
	plan.SQL = string(edits)
	return plan, nil
}

func CreateLyraSpell(realm *lyracore.Realm, dataDir string, base int, starter, name string) (SpellCreate, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SpellCreate{}, refuse("name", "a new spell needs a name")
	}
	var source map[string]string
	if base > 0 {
		record, err := ReadLyraSpell(realm, dataDir, base)
		if err != nil {
			return SpellCreate{}, err
		}
		source = record.Values
	} else {
		schema, err := worldSpellSource(133)
		if err != nil {
			return SpellCreate{}, err
		}
		tables, err := ReadSpellTables(dataDir)
		if err != nil {
			return SpellCreate{}, err
		}
		like := ""
		for _, choice := range spellStarters {
			if choice.ID == starter {
				like = choice.Like
			}
		}
		visual, icon, err := spellLook(dataDir, like)
		if err != nil {
			return SpellCreate{}, err
		}
		source, err = starterValues(lyraSpellColumns(schema), starter, tables, visual, icon)
		if err != nil {
			return SpellCreate{}, err
		}
		source["build"] = strconv.Itoa(spellBuild)
	}
	table, err := realm.DB.Query(realm.Database, "SELECT spell_id FROM game_spell")
	if err != nil {
		return SpellCreate{}, err
	}
	used := map[uint32]bool{}
	if err := table.Each(func(r *stdb.Row) error { used[uint32(r.Uint("spell_id"))] = true; return r.Err }); err != nil {
		return SpellCreate{}, err
	}
	sb, err := readClientFile(dataDir, SpellPath)
	if err != nil {
		return SpellCreate{}, err
	}
	client, err := parseDBC(sb)
	if err != nil {
		return SpellCreate{}, err
	}
	for r := 0; r < client.records; r++ {
		used[client.u32(r, 0)] = true
	}
	entry := uint32(lyraSpellFloor)
	for entry <= lyraSpellCeil && used[entry] {
		entry++
	}
	if entry > lyraSpellCeil {
		return SpellCreate{}, refuse("entry", "the Operator Spell Range is full")
	}
	source["entry"], source["name"], source["script_name"] = strconv.FormatUint(uint64(entry), 10), name, ""
	cols := lyraSpellColumns(source)
	out, err := spellClientFile(sb, int(entry), base, cols, source)
	if err != nil {
		return SpellCreate{}, err
	}
	edits, err := compileLyraSpell(dataDir, lyraSpellRequest{Create: true, Source: source, Trainers: []map[string]json.RawMessage{}})
	if err != nil {
		return SpellCreate{}, err
	}
	if err := applyLyraSpell(realm, edits); err != nil {
		return SpellCreate{}, err
	}
	return SpellCreate{Entry: int(entry), SQL: string(edits), File: mpq.File{Name: SpellPath, Data: out}}, nil
}
