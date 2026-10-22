package main

import (
	"fmt"
	"strings"

	"Meld/internal/config"
	"Meld/internal/lyracore"
)

// LyraCoreInfo is the LyraCore realm's settings as the dialog shows them: the token only as
// whether one is saved.
type LyraCoreInfo struct {
	URL      string `json:"url"`
	Database string `json:"database"`
	Shards   string `json:"shards"`
	HasToken bool   `json:"hasToken"`
}

// LyraCore returns the LyraCore realm's settings; an empty URL for none.
func (a *App) LyraCore() LyraCoreInfo {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	l := a.cfg.LyraCore
	return LyraCoreInfo{URL: l.URL, Database: l.Database, Shards: l.Shards, HasToken: l.Token != ""}
}

// SetLyraCore saves the LyraCore realm Meld reads and writes spawns in, once its default database
// answers a query with the token. An empty URL clears it; an empty token keeps the saved one.
func (a *App) SetLyraCore(url, token, database, shards string) error {
	url = strings.TrimSpace(url)
	a.cfgMu.Lock()
	l := config.LyraCore{URL: url, Token: strings.TrimSpace(token), Database: strings.TrimSpace(database), Shards: strings.TrimSpace(shards)}
	if l.Token == "" {
		l.Token = a.cfg.LyraCore.Token
	}
	a.cfgMu.Unlock()
	if url == "" {
		l = config.LyraCore{}
	} else {
		realm, err := lyracore.Open(lyraSettings(l))
		if err != nil {
			return err
		}
		if err := realm.DB.Ping(); err != nil {
			return fmt.Errorf("the SpacetimeDB server does not answer at %s: %w", url, err)
		}
		if _, err := realm.DB.Query(realm.Database(0), "SELECT COUNT(*) AS n FROM game_creature_template"); err != nil {
			return fmt.Errorf("the realm's database %s can't be read: %w", realm.Database(0), err)
		}
	}
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	a.cfg.LyraCore = l
	return config.Update(func(c *config.Config) error {
		c.LyraCore = l
		return nil
	})
}

// lyraRealm is the configured LyraCore realm, nil when none is set.
func (a *App) lyraRealm() (*lyracore.Realm, error) {
	a.cfgMu.Lock()
	l := a.cfg.LyraCore
	a.cfgMu.Unlock()
	if l.URL == "" {
		return nil, nil
	}
	return lyracore.Open(lyraSettings(l))
}

func lyraSettings(l config.LyraCore) lyracore.Settings {
	return lyracore.Settings{URL: l.URL, Token: l.Token, Database: l.Database, Shards: l.Shards}
}
