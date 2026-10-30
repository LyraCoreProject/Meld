package main

import (
	"fmt"
	"os"

	"Meld/internal/mpq"
	"Meld/internal/vmangos"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// SpellTexts gives the description of each spell, its $ tokens filled in, for the Shop tab's tooltips.
func (a *App) SpellTexts(ids []int) (map[int]string, error) {
	dataDir, err := a.clientDataDir()
	if err != nil {
		return nil, err
	}
	return vmangos.SpellTexts(dataDir, ids)
}

// SearchSpells lists the client's spells by name or ID, with their rank, icon and text, for the
// creature editor's Spells tab.
func (a *App) SearchSpells(text string) ([]vmangos.SpellRef, error) {
	dataDir, err := a.clientDataDir()
	if err != nil {
		return nil, err
	}
	return vmangos.SearchSpells(dataDir, text, 80)
}

// SpellCategories lists the categories the creature editor's Spells tab browses the client's
// spells by (#340): the creature abilities, all spells, and the class, pet and profession skill lines.
func (a *App) SpellCategories() ([]vmangos.SpellCategory, error) {
	dataDir, err := a.clientDataDir()
	if err != nil {
		return nil, err
	}
	return vmangos.SpellCategories(dataDir)
}

// ListSpells lists the client's spells in a category and school (-1 any), narrowed by a name or ID,
// at most limit of them with the count that matched, for the creature editor's Spells tab (#340).
func (a *App) ListSpells(category string, school int, text string, limit int) (vmangos.SpellList, error) {
	dataDir, err := a.clientDataDir()
	if err != nil {
		return vmangos.SpellList{}, err
	}
	return vmangos.ListSpells(dataDir, category, school, text, min(max(limit, 1), 2000))
}

// SpellRefs gives each of the client's spells by ID, with its rank, icon and text.
func (a *App) SpellRefs(ids []int) (map[int]vmangos.SpellRef, error) {
	dataDir, err := a.clientDataDir()
	if err != nil {
		return nil, err
	}
	return vmangos.SpellRefs(dataDir, ids)
}

// Reads the full editable source row for the selected Realm or VMaNGOS world.
func (a *App) ReadSpell(entry int) (vmangos.SpellRecord, error) {
	if realm, dataDir, err := a.lyraRealmAndData(); err != nil {
		return vmangos.SpellRecord{}, err
	} else if realm != nil {
		return vmangos.ReadLyraSpell(realm, dataDir, entry)
	}
	server, dataDir, err := a.serverAndData()
	if err != nil {
		return vmangos.SpellRecord{}, err
	}
	return vmangos.ReadSpell(server, dataDir, entry)
}

// SQL contains VMaNGOS SQL or the Realm reducer arguments.
type SpellSaved struct {
	Live        bool                 `json:"live"`
	SQL         string               `json:"sql"`
	IconAdded   string               `json:"iconAdded"`
	Visual      int                  `json:"visual"`
	VisualAdded bool                 `json:"visualAdded"`
	Refused     *vmangos.ColumnError `json:"refused,omitempty"`
}

// Saves the world first; a client patch failure can be retried with Save.
func (a *App) SaveSpell(entry, build int, values map[string]string, icon string, visual *vmangos.VisualDesign) (SpellSaved, error) {
	if realm, dataDir, err := a.lyraRealmAndData(); err != nil {
		return SpellSaved{}, err
	} else if realm != nil {
		if a.sandboxed() {
			return SpellSaved{}, errSandboxed
		}
		plan, err := vmangos.SaveLyraSpell(realm, dataDir, entry, build, values, icon, visual)
		if refused := vmangos.Refused(err); refused != nil {
			return SpellSaved{Refused: refused}, nil
		}
		if err != nil {
			return SpellSaved{}, err
		}
		err = a.saveClientFiles(dataDir, plan.Files)
		vmangos.ForgetSpells()
		if err != nil {
			return SpellSaved{}, fmt.Errorf("the spell is saved in the Realm, but writing patch-4 failed; Save again to retry: %w", err)
		}
		return SpellSaved{Live: true, SQL: plan.SQL, IconAdded: plan.IconAdded, Visual: plan.Visual, VisualAdded: plan.VisualAdded}, nil
	}
	server, dataDir, err := a.serverForWrite()
	if err != nil {
		return SpellSaved{}, err
	}
	plan, err := vmangos.PlanSpellSave(server, dataDir, entry, build, values, icon, visual)
	if r := vmangos.Refused(err); r != nil {
		return SpellSaved{Refused: r}, nil
	}
	if err != nil {
		return SpellSaved{}, err
	}
	if err := vmangos.SaveSpellSQL(server, plan.SQL); err != nil {
		return SpellSaved{}, err
	}
	a.reloadSpells(server)
	err = a.saveClientFiles(dataDir, plan.Files)
	vmangos.ForgetSpells()
	if err != nil {
		return SpellSaved{}, fmt.Errorf("the spell is saved in the world database, but writing it into the client's patch failed (Save again to retry): %w", err)
	}
	return SpellSaved{SQL: plan.SQL, IconAdded: plan.IconAdded, Visual: plan.Visual, VisualAdded: plan.VisualAdded}, nil
}

// Returns the new entry even if the subsequent client patch write fails.
func (a *App) CreateSpell(base int, starter, name string) (int, error) {
	if realm, dataDir, err := a.lyraRealmAndData(); err != nil {
		return 0, err
	} else if realm != nil {
		if a.sandboxed() {
			return 0, errSandboxed
		}
		plan, err := vmangos.CreateLyraSpell(realm, dataDir, base, starter, name)
		if err != nil {
			return 0, err
		}
		err = a.saveClientFiles(dataDir, []mpq.File{plan.File})
		vmangos.ForgetSpells()
		if err != nil {
			return plan.Entry, fmt.Errorf("spell %d is saved in the Realm, but writing patch-4 failed; open it by id and Save to retry: %w", plan.Entry, err)
		}
		return plan.Entry, nil
	}
	server, dataDir, err := a.serverForWrite()
	if err != nil {
		return 0, err
	}
	plan, err := vmangos.PlanCreateSpell(server, dataDir, base, starter, name)
	if err != nil {
		return 0, err
	}
	if err := vmangos.SaveSpellSQL(server, plan.SQL); err != nil {
		return 0, err
	}
	a.reloadSpells(server)
	err = a.saveClientFiles(dataDir, []mpq.File{plan.File})
	vmangos.ForgetSpells()
	if err != nil {
		return plan.Entry, fmt.Errorf("spell %d is made in the world database, but writing it into the client's patch failed (Save it to retry): %w", plan.Entry, err)
	}
	return plan.Entry, nil
}

// Returns the chosen file path, or an empty path after cancellation.
func (a *App) ExportSpells(entries []int, suggested string) (string, error) {
	if realm, err := a.lyraRealm(); err != nil {
		return "", err
	} else if realm != nil {
		return "", fmt.Errorf("SQL spell import and export are unavailable on a Realm; use the spell editor")
	}
	server, dataDir, err := a.serverAndData()
	if err != nil {
		return "", err
	}
	script, err := vmangos.ExportSpells(server, dataDir, entries)
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Export Spells", DefaultFilename: suggested + ".sql", Filters: sqlFilters})
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, []byte(script), 0o644)
}

// OpenSpellsDialog asks for an SQL script of spells to import. Returns "" if cancelled.
func (a *App) OpenSpellsDialog() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Import Spells", Filters: sqlFilters})
}

func (a *App) PlanSpellImport(path string) (vmangos.SpellImport, error) {
	if realm, err := a.lyraRealm(); err != nil {
		return vmangos.SpellImport{}, err
	} else if realm != nil {
		return vmangos.SpellImport{}, fmt.Errorf("SQL spell import and export are unavailable on a Realm; use the spell editor")
	}
	server, dataDir, err := a.serverAndData()
	if err != nil {
		return vmangos.SpellImport{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return vmangos.SpellImport{}, err
	}
	return vmangos.PlanSpellImport(server, dataDir, string(b))
}

// Realm sessions refuse SQL scripts; VMaNGOS imports also update the client patch.
func (a *App) ImportSpells(path string) ([]int, error) {
	if realm, err := a.lyraRealm(); err != nil {
		return nil, err
	} else if realm != nil {
		return nil, fmt.Errorf("SQL spell import and export are unavailable on a Realm; use the spell editor")
	}
	server, dataDir, err := a.serverForWrite()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entries, looks, err := vmangos.ImportSpellRows(server, dataDir, string(b))
	if err != nil {
		return nil, err
	}
	a.reloadSpells(server)
	files, err := vmangos.SpellClientFiles(server, dataDir, entries)
	if err == nil {
		err = a.saveClientFiles(dataDir, append(looks, files...))
	}
	vmangos.ForgetSpells()
	if err != nil {
		return entries, fmt.Errorf("the spells are in the world database, but writing them into the client's Spell.dbc failed (Save each one, or import again, to retry): %w", err)
	}
	return entries, nil
}

// ReadSpellVisual reads a client spell visual as the composer edits it (#361).
func (a *App) ReadSpellVisual(id int) (vmangos.VisualDesign, error) {
	dataDir, err := a.clientDataDir()
	if err != nil {
		return vmangos.VisualDesign{}, err
	}
	return vmangos.ReadSpellVisual(dataDir, id)
}

// ListVisualEffects lists the client's spell effect models, for the composer's pickers.
func (a *App) ListVisualEffects() ([]vmangos.VisualEffect, error) {
	dataDir, err := a.clientDataDir()
	if err != nil {
		return nil, err
	}
	return vmangos.ListVisualEffects(dataDir)
}

// SpellStarters lists the kinds of spell New spell can start from without copying another (#360).
func (a *App) SpellStarters() []vmangos.SpellStarter {
	return vmangos.SpellStarters()
}

// SpellTables lists the client's cast times, durations, ranges and radii, for the spell editor's selects.
func (a *App) SpellTables() (vmangos.SpellTables, error) {
	dataDir, err := a.clientDataDir()
	if err != nil {
		return vmangos.SpellTables{}, err
	}
	return vmangos.ReadSpellTables(dataDir)
}

// SpellsWithVisual names a few of the client's spells that use a spell visual, for the spell editor.
func (a *App) SpellsWithVisual(visual int) ([]vmangos.SpellRef, error) {
	dataDir, err := a.clientDataDir()
	if err != nil {
		return nil, err
	}
	return vmangos.SpellsWithVisual(dataDir, visual, 5)
}

func (a *App) ReadCreatureSpells(entry int) (vmangos.CreatureSpells, error) {
	if realm, err := a.lyraRealm(); err != nil {
		return vmangos.CreatureSpells{}, err
	} else if realm != nil {
		return vmangos.ReadLyraCreatureSpells(realm, entry)
	}
	server, _, err := a.serverAndData()
	if err != nil {
		return vmangos.CreatureSpells{}, err
	}
	return vmangos.ReadCreatureSpells(server, entry)
}

// Returns the current list id, SQL or reducer arguments, unsupported values and any Refusal.
func (a *App) SaveCreatureSpells(entry int, spells []vmangos.CreatureSpell) (vmangos.CreatureSpellsSaved, error) {
	if realm, err := a.lyraRealm(); err != nil {
		return vmangos.CreatureSpellsSaved{}, err
	} else if realm != nil {
		if a.sandboxed() {
			return vmangos.CreatureSpellsSaved{}, errSandboxed
		}
		res, err := vmangos.SaveLyraCreatureSpells(realm, entry, spells)
		saved := vmangos.CreatureSpellsSaved{Live: res.Live, SQL: res.SQL, Skipped: res.Skipped, Refused: vmangos.Refused(err)}
		if len(spells) > 0 {
			saved.ListID = entry
		}
		if saved.Refused != nil {
			return saved, nil
		}
		return saved, err
	}
	server, _, err := a.serverForWrite()
	if err != nil {
		return vmangos.CreatureSpellsSaved{}, err
	}
	return vmangos.SaveCreatureSpells(server, entry, spells)
}
