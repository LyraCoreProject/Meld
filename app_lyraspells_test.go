package main

import (
	"strings"
	"testing"
)

func TestRealmSpellScriptsStopBeforeFileOrSQLAccess(t *testing.T) {
	app := &App{lyraSession: &lyraSession{url: "http://127.0.0.1:1", database: "test"}}
	_, exportErr := app.ExportSpells([]int{133}, "spells")
	_, planErr := app.PlanSpellImport("does-not-exist.sql")
	_, importErr := app.ImportSpells("does-not-exist.sql")
	for _, err := range []error{exportErr, planErr, importErr} {
		if err == nil || !strings.Contains(err.Error(), "unavailable on a Realm") {
			t.Fatalf("wrong result: %v", err)
		}
	}
}
