package vmangos

import (
	"errors"
	"strings"
	"time"

	"Meld/internal/lyracore"
	"Meld/internal/stdb"
)

// lyraLogoutRefusal is the realm's Refusal for a character whose game has closed but whose logout
// has not run yet (LyraCore docs/operator-content-api.md, Character teleport).
const lyraLogoutRefusal = "retry after logout"

// How long and how often PlaceLyraCharacter asks again while the realm logs the character out.
var (
	lyraLogoutWait  = 10 * time.Second
	lyraLogoutRetry = 250 * time.Millisecond
)

// TeleportLyraCharacter moves the named character to spot through the realm's
// operator_teleport_character: in the world it is teleported there, logged out its saved position
// is set, so the next login enters there. A Refusal comes back as the realm's message alone.
func TeleportLyraCharacter(realm *lyracore.Realm, name string, spot CharacterSpot) error {
	err := realm.DB.Call(realm.Database, "operator_teleport_character", name, spot.Map, float32(spot.X), float32(spot.Y), float32(spot.Z), float32(spot.O))
	var response *stdb.HTTPError
	if errors.As(err, &response) && response.StatusCode == stdb.ReducerFailedStatus && response.Body != "" {
		return errors.New(response.Body)
	}
	return err
}

// PlaceLyraCharacter is TeleportLyraCharacter for a character whose game was just closed: while the
// realm still holds it online without a world entity, it asks again until the logout has run.
func PlaceLyraCharacter(realm *lyracore.Realm, name string, spot CharacterSpot) error {
	deadline := time.Now().Add(lyraLogoutWait)
	for {
		err := TeleportLyraCharacter(realm, name, spot)
		if err == nil || !strings.Contains(err.Error(), lyraLogoutRefusal) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(lyraLogoutRetry)
	}
}
