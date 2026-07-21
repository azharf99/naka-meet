package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/naka-meet/sfu/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateAndValidateToken_UUIDv7(t *testing.T) {
	secret := []byte("super-secret-key-12345")

	// Generate UUID v7 for user ID
	userIDUUID, err := uuid.NewV7()
	require.NoError(t, err)
	userID := userIDUUID.String()
	role := "host"

	// 1. Generate Token
	tokenString, err := auth.GenerateToken(userID, role, secret, 1*time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenString)

	// 2. Validate Token
	claims, err := auth.ValidateToken(tokenString, secret)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, role, claims.Role)
}

func TestValidateToken_InvalidSecret(t *testing.T) {
	secret := []byte("correct-secret")
	wrongSecret := []byte("wrong-secret")

	userIDUUID, _ := uuid.NewV7()
	tokenString, err := auth.GenerateToken(userIDUUID.String(), "participant", secret, 1*time.Hour)
	require.NoError(t, err)

	_, err = auth.ValidateToken(tokenString, wrongSecret)
	assert.Error(t, err)
}

func TestValidateToken_Expired(t *testing.T) {
	secret := []byte("secret")

	userIDUUID, _ := uuid.NewV7()
	// Expired token (-1 minute)
	tokenString, err := auth.GenerateToken(userIDUUID.String(), "participant", secret, -1*time.Minute)
	require.NoError(t, err)

	_, err = auth.ValidateToken(tokenString, secret)
	assert.Error(t, err)
}

func TestAuth_GenerateAndValidateTokenWithName(t *testing.T) {
	secret := []byte("super-secret-key-12345")

	userIDUUID, err := uuid.NewV7()
	require.NoError(t, err)
	userID := userIDUUID.String()
	name := "Budi"
	role := "guest"

	tokenString, err := auth.GenerateTokenWithName(userID, name, role, secret, 1*time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenString)

	claims, err := auth.ValidateToken(tokenString, secret)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, name, claims.Name)
	assert.Equal(t, role, claims.Role)
}

