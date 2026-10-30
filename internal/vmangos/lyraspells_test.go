package vmangos

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"Meld/internal/lyracore"
	"Meld/internal/mpq"
	"Meld/internal/stdb"
)

func TestLyraSpellFullSaveAndReadback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the compiler process fixture uses a POSIX executable script")
	}
	data := t.TempDir()
	record := make([]uint32, 173)
	record[0] = 133
	files := []mpq.File{{Name: SpellPath, Data: dbcBytes(173, [][]uint32{record}, "\x00")},
		{Name: SpellIconPath, Data: dbcBytes(2, [][]uint32{{1, 1}}, "\x00Interface\\Icons\\Old\x00")}}
	for _, name := range []string{"SpellCastTimes.dbc", "SpellRange.dbc", "SpellDuration.dbc", "SpellRadius.dbc"} {
		files = append(files, mpq.File{Name: `DBFilesClient\` + name, Data: []byte("fixture")})
	}
	if err := os.WriteFile(filepath.Join(data, "base.MPQ"), mpq.Write(files), 0600); err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(t.TempDir(), "compiler")
	script := `#!/usr/bin/env python3
import json, pathlib, sys
assert sys.argv[1:4] == ['--compile-spell-edits', '-', '--dbc']
assert len(list(pathlib.Path(sys.argv[4]).glob('*.dbc'))) == 4
request = json.load(sys.stdin)[0]
source = request['source']
assert source['customFlags'] == '1024'
assert source['effectBonusCoefficient1'] == '0.5'
print(json.dumps([{'create': request['create'], 'spell': {'spell_id': int(source['entry']), 'name':source['name']}, 'source':[{'name':k,'value':v} for k,v in source.items()], 'trainers':request['trainers']}]))
`
	if err := os.WriteFile(compiler, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MELD_LYRACORE_SPELL_COMPILER", compiler)
	source := []lyraSpellSourceField{{"entry", "133"}, {"build", "5875"}, {"name", "Fireball"}, {"customFlags", "1024"}, {"effectBonusCoefficient1", "0.5"}, {"spellFamilyFlags", "0"}, {"spellIconId", "1"}}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/call/operator_apply_spell_edits") {
			var args [][]struct {
				Source []lyraSpellSourceField `json:"source"`
			}
			if err := json.Unmarshal(b, &args); err != nil || len(args) != 1 || len(args[0]) != 1 {
				t.Errorf("body %s: %v", b, err)
				w.WriteHeader(400)
				return
			}
			source = args[0][0].Source
			calls++
			return
		}
		if !strings.HasPrefix(string(b), "SELECT ") {
			t.Errorf("SQL write: %s", b)
		}
		switch {
		case strings.Contains(string(b), "game_operator_spell_source"):
			fields, _ := json.Marshal(source)
			writeLyraTable(t, w, stdb.Table{Columns: []string{"fields"}, Rows: [][]json.RawMessage{{fields}}})
		case strings.Contains(string(b), "game_trainer_spell"):
			writeLyraTable(t, w, stdb.Table{Columns: []string{"id"}})
		default:
			writeLyraTable(t, w, stdb.Table{Columns: []string{"spell_id"}, Rows: [][]json.RawMessage{{json.RawMessage("133")}}})
		}
	}))
	defer server.Close()
	realm := &lyracore.Realm{DB: &stdb.Client{URL: server.URL}, Database: "test"}
	saved, err := SaveLyraSpell(realm, data, 133, 5875, map[string]string{"name": "Edited bolt", "spellFamilyFlags": "18446744073709551615"}, "NewIcon", nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || saved.IconAdded != "NewIcon" || len(saved.Files) != 2 {
		t.Fatalf("save %+v, calls %d", saved, calls)
	}
	client, err := parseDBC(saved.Files[1].Data)
	if err != nil {
		t.Fatal(err)
	}
	row, _ := client.row(133)
	if client.str(row, spellFName) != "Edited bolt" {
		t.Fatal("client name was not saved")
	}
	reread, err := ReadLyraSpell(realm, data, 133)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Values["spellFamilyFlags"] != "18446744073709551615" || reread.Values["effectBonusCoefficient1"] != "0.5" {
		t.Fatalf("readback: %+v", reread)
	}

	created, err := CreateLyraSpell(realm, data, 133, "", "Copied bolt")
	if err != nil {
		t.Fatal(err)
	}
	if created.Entry != 60000 || calls != 2 {
		t.Fatalf("create: %+v, calls %d", created, calls)
	}
	copied, err := parseDBC(created.File.Data)
	if err != nil {
		t.Fatal(err)
	}
	copiedRow, ok := copied.row(uint32(created.Entry))
	if !ok || copied.str(copiedRow, spellFName) != "Copied bolt" {
		t.Fatal("new spell missing from client file")
	}
}

func TestLyraSpellRefusalIsDistinctFromTransportLoss(t *testing.T) {
	for _, status := range []int{530, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "spell cannot be saved", status) }))
			defer server.Close()
			realm := &lyracore.Realm{DB: &stdb.Client{URL: server.URL}, Database: "test"}
			err := applyLyraSpell(realm, json.RawMessage(`[]`))
			if err == nil || (Refused(err) != nil) != (status == 530) {
				t.Fatalf("status %d: %v", status, err)
			}
		})
	}
}
