package turn_test

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/naka-meet/sfu/pkg/turn"
	"github.com/stretchr/testify/assert"
)

func TestGenerateCredentials_UsernameEncodesExpiryAndUserID(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	username, _ := turn.GenerateCredentials("shared-secret", "user-123", 12*time.Hour, now)

	wantExpiry := now.Add(12 * time.Hour).Unix()
	assert.Equal(t, fmt.Sprintf("%d:user-123", wantExpiry), username)
}

func TestGenerateCredentials_PasswordMatchesCoturnsOwnDerivation(t *testing.T) {
	// coturn independently recomputes HMAC-SHA1(secret, username) and
	// compares it to the password it was handed — this test recomputes
	// that same derivation itself to guard against ever drifting from the
	// exact scheme (SHA1 specifically — not SHA256 — base64 standard, not
	// URL-safe) coturn's use-auth-secret mode expects.
	now := time.Now()
	username, password := turn.GenerateCredentials("shared-secret", "user-123", time.Hour, now)

	mac := hmac.New(sha1.New, []byte("shared-secret"))
	mac.Write([]byte(username))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	assert.Equal(t, want, password)
}

func TestGenerateCredentials_DifferentSecretsProduceDifferentPasswords(t *testing.T) {
	now := time.Now()
	_, passwordA := turn.GenerateCredentials("secret-a", "user-123", time.Hour, now)
	_, passwordB := turn.GenerateCredentials("secret-b", "user-123", time.Hour, now)
	assert.NotEqual(t, passwordA, passwordB)
}

func TestGenerateCredentials_DifferentUsersProduceDifferentCredentials(t *testing.T) {
	now := time.Now()
	usernameA, passwordA := turn.GenerateCredentials("shared-secret", "user-a", time.Hour, now)
	usernameB, passwordB := turn.GenerateCredentials("shared-secret", "user-b", time.Hour, now)
	assert.NotEqual(t, usernameA, usernameB)
	assert.NotEqual(t, passwordA, passwordB)
}

func TestGenerateCredentials_TTLIsReflectedInExpiryTimestamp(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	shortUsername, _ := turn.GenerateCredentials("shared-secret", "user-123", time.Minute, now)
	longUsername, _ := turn.GenerateCredentials("shared-secret", "user-123", 24*time.Hour, now)
	assert.NotEqual(t, shortUsername, longUsername, "different TTLs must produce different expiry timestamps")
}
