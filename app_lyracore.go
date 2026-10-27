package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"Meld/internal/config"
	"Meld/internal/lyracore"
)

// The environment `lyracore editor` starts Meld with: the local realm and client, for this session
// only. The token comes as the path of the realm's owner token file, so it is in no environment.
const (
	envLyraURL       = "MELD_LYRACORE_URL"
	envLyraDatabase  = "MELD_LYRACORE_DATABASE"
	envLyraTokenFile = "MELD_LYRACORE_TOKEN_FILE"
	envWowPath       = "MELD_WOW_PATH"
)

// lyraSession is the realm the environment names; nil when it names none.
type lyraSession struct {
	url, database, tokenFile string
}

func lyraSessionFromEnv() *lyraSession {
	url := strings.TrimSpace(os.Getenv(envLyraURL))
	if url == "" {
		return nil
	}
	return &lyraSession{url: url, database: strings.TrimSpace(os.Getenv(envLyraDatabase)), tokenFile: strings.TrimSpace(os.Getenv(envLyraTokenFile))}
}

// settings reads the token file each time, so a token `lyracore dev up` mints again is picked up.
func (s *lyraSession) settings() (lyracore.Settings, error) {
	l := lyracore.Settings{URL: s.url, Database: s.database}
	if s.tokenFile != "" {
		b, err := os.ReadFile(s.tokenFile)
		if err != nil {
			return l, fmt.Errorf("the realm's token can't be read: %w", err)
		}
		l.Token = strings.TrimSpace(string(b))
	}
	return l, nil
}

// LyraCoreInfo is the LyraCore realm's settings as the dialog shows them: the token only as
// whether one is saved. Session says `lyracore editor` set them for this session.
type LyraCoreInfo struct {
	URL      string `json:"url"`
	Database string `json:"database"`
	HasToken bool   `json:"hasToken"`
	Session  bool   `json:"session"`
}

// LyraCore returns the LyraCore realm's settings; an empty URL for none.
func (a *App) LyraCore() LyraCoreInfo {
	if s := a.lyraSession; s != nil {
		return LyraCoreInfo{URL: s.url, Database: s.database, HasToken: s.tokenFile != "", Session: true}
	}
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	l := a.cfg.LyraCore
	return LyraCoreInfo{URL: l.URL, Database: l.Database, HasToken: l.Token != ""}
}

// SetLyraCore saves the LyraCore realm Meld reads and writes spawns in, once its database answers
// a query with the token. An empty URL clears it; an empty token keeps the saved one.
func (a *App) SetLyraCore(url, token, database string) error {
	if a.lyraSession != nil {
		return errors.New("`lyracore editor` set this session's realm; start Meld on its own to choose another")
	}
	url = strings.TrimSpace(url)
	a.cfgMu.Lock()
	l := config.LyraCore{URL: url, Token: strings.TrimSpace(token), Database: strings.TrimSpace(database)}
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
		if _, err := realm.DB.Query(realm.Database, "SELECT COUNT(*) AS n FROM game_creature_template"); err != nil {
			return fmt.Errorf("the realm's database %s can't be read: %w", realm.Database, err)
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

// lyraRealm is the session's LyraCore realm, else the configured one; nil when there is none.
func (a *App) lyraRealm() (*lyracore.Realm, error) {
	if s := a.lyraSession; s != nil {
		l, err := s.settings()
		if err != nil {
			return nil, err
		}
		return lyracore.Open(l)
	}
	a.cfgMu.Lock()
	l := a.cfg.LyraCore
	a.cfgMu.Unlock()
	if l.URL == "" {
		return nil, nil
	}
	return lyracore.Open(lyraSettings(l))
}

// lyraRealmAndData is the LyraCore realm (nil for none) and, with one, the client's data folder.
func (a *App) lyraRealmAndData() (*lyracore.Realm, string, error) {
	realm, err := a.lyraRealm()
	if err != nil || realm == nil {
		return nil, "", err
	}
	dataDir, err := a.clientDataDir()
	return realm, dataDir, err
}

// lyraPlaceholderGUID stands for a new spawn's guid until the save: the realm gives the real one,
// and the editor numbers its new spawns above every guid it has.
const lyraPlaceholderGUID = 1

func lyraSettings(l config.LyraCore) lyracore.Settings {
	return lyracore.Settings{URL: l.URL, Token: l.Token, Database: l.Database}
}
