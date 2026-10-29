package vmangos

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Meld/internal/lyracore"
	"Meld/internal/stdb"
)

// The call names the reducer and its arguments in order; a Refusal is the realm's message alone.
func TestTeleportLyraCharacterCallsTheReducer(t *testing.T) {
	var path, body string
	refusal := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		path, body = r.URL.Path, string(b)
		if refusal != "" {
			http.Error(w, refusal, stdb.ReducerFailedStatus)
		}
	}))
	defer server.Close()
	realm := &lyracore.Realm{DB: &stdb.Client{URL: server.URL}, Database: "lyracore"}
	spot := CharacterSpot{Map: 0, X: -8949.5, Y: -132.25, Z: 83.5, O: 1.5}
	if err := TeleportLyraCharacter(realm, "Sizaro", spot); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/database/lyracore/call/operator_teleport_character" || body != `["Sizaro",0,-8949.5,-132.25,83.5,1.5]` {
		t.Fatalf("called %s with %s", path, body)
	}
	refusal = "character is in combat"
	if err := TeleportLyraCharacter(realm, "Sizaro", spot); err == nil || err.Error() != refusal {
		t.Fatalf("refusal: %v", err)
	}
}

// After the game closes, the realm may still hold the character online until its logout runs.
func TestPlaceLyraCharacterWaitsForTheLogout(t *testing.T) {
	oldWait, oldRetry := lyraLogoutWait, lyraLogoutRetry
	lyraLogoutWait, lyraLogoutRetry = time.Second, time.Millisecond
	t.Cleanup(func() { lyraLogoutWait, lyraLogoutRetry = oldWait, oldRetry })
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, "character is online without a world entity; retry after logout", stdb.ReducerFailedStatus)
		}
	}))
	defer server.Close()
	realm := &lyracore.Realm{DB: &stdb.Client{URL: server.URL}, Database: "lyracore"}
	if err := PlaceLyraCharacter(realm, "Sizaro", CharacterSpot{}); err != nil || calls != 3 {
		t.Fatalf("%d calls, %v", calls, err)
	}
	calls = 0
	lyraLogoutWait = 0
	if err := PlaceLyraCharacter(realm, "Sizaro", CharacterSpot{}); err == nil || calls != 1 {
		t.Fatalf("past the wait: %d calls, %v", calls, err)
	}
}
