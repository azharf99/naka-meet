// Package turn generates time-limited TURN credentials for coturn's
// "use-auth-secret" REST API scheme (a single shared secret configured on
// both the SFU and the coturn server — see docker-compose.yml's coturn
// service — rather than a fixed username/password embedded in every
// client's ICE config). A long-lived static credential shipped in the
// frontend bundle would let anyone who views the page's source relay
// unlimited traffic through the TURN server indefinitely; a credential
// that expires in a few hours bounds that to whoever's actively in a
// call.
package turn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"time"
)

// GenerateCredentials implements coturn's REST API credential mechanism:
// the username is "<unix-expiry>:<userID>" and the password is
// base64(HMAC-SHA1(secret, username)) — coturn derives the same password
// itself from the username it's handed and the secret it was configured
// with, so nothing needs to be stored server-side per credential.
//
// now is passed in (not read internally via time.Now()) so the expiry
// calculation is directly testable without depending on wall-clock time.
func GenerateCredentials(secret, userID string, ttl time.Duration, now time.Time) (username, password string) {
	expiry := now.Add(ttl).Unix()
	username = fmt.Sprintf("%d:%s", expiry, userID)

	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	password = base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return username, password
}
