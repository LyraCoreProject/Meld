package clientpatch

import (
	"path/filepath"
	"strings"
	"sync"
)

const Default = "patch-3.MPQ"
const LyraCore = "patch-4.MPQ"

var realms sync.Map

func key(dataDir string) string { return strings.ToLower(filepath.Clean(dataDir)) }

// Configure selects Meld's archive for this client folder, including its pending files.
func Configure(dataDir string, lyra bool) {
	if lyra {
		realms.Store(key(dataDir), true)
	} else {
		realms.Delete(key(dataDir))
	}
}

func Name(dataDir string) string {
	if _, ok := realms.Load(key(dataDir)); ok {
		return LyraCore
	}
	return Default
}
