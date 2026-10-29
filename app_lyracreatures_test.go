package main

import (
	"strings"
	"testing"

	"Meld/internal/vmangos"
)

func TestLyraCreatureSandboxWrites(t *testing.T) {
	a := &App{lyraSession: &lyraSession{url: "http://invalid"}, sandboxData: "sandbox"}
	for name, call := range map[string]func() error{
		"save":   func() error { _, err := a.SaveCreature(1, 0, map[string]string{"npc_flags": "129"}); return err },
		"create": func() error { _, err := a.CreateCreature(0, "New"); return err },
		"spells": func() error { _, err := a.SaveCreatureSpells(1, nil); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err != errSandboxed {
				t.Fatalf("sandbox: %v", err)
			}
		})
	}
}

func TestLyraUnavailableEditorsDoNotUseVMaNGOS(t *testing.T) {
	a := &App{}
	a.cfg.LyraCore.URL = "http://invalid"
	a.cfg.VmangosPath = "must-not-read"
	for name, call := range map[string]func() error{
		"object read":     func() error { _, err := a.ReadObject(1); return err },
		"object save":     func() error { _, err := a.SaveObject(1, 0, nil); return err },
		"quest read":      func() error { _, err := a.ReadQuest(1); return err },
		"quest save":      func() error { _, err := a.SaveQuest(1, 0, nil); return err },
		"creature quests": func() error { _, err := a.ReadCreatureQuests(1); return err },
		"save quests":     func() error { _, err := a.SaveCreatureQuests(1, vmangos.QuestLists{}); return err },
		"class stats":     func() error { _, err := a.ClassLevelStats(1, 1, 10); return err },
		"equipment":       func() error { _, err := a.EquipmentFor([3]int{1, 0, 0}); return err },
		"equipment items": func() error { _, err := a.EquipmentItems(1); return err },
		"weapons":         func() error { _, err := a.ListWeapons(); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil || !strings.Contains(err.Error(), "LyraCore realm") || strings.Contains(err.Error(), "VMaNGOS") {
				t.Fatalf("fallback to VMaNGOS: %v", err)
			}
		})
	}
}
