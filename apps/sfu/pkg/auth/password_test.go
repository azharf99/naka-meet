package auth_test

import (
	"testing"

	"github.com/naka-meet/sfu/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPassword_ProducesVerifiableHash(t *testing.T) {
	hash, err := auth.HashPassword("correct-password-123")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.NotEqual(t, "correct-password-123", hash)
}

func TestComparePassword_SucceedsForCorrectPassword(t *testing.T) {
	hash, err := auth.HashPassword("correct-password-123")
	require.NoError(t, err)

	err = auth.ComparePassword(hash, "correct-password-123")
	assert.NoError(t, err)
}

func TestComparePassword_FailsForWrongPassword(t *testing.T) {
	hash, err := auth.HashPassword("correct-password-123")
	require.NoError(t, err)

	err = auth.ComparePassword(hash, "wrong-password")
	assert.ErrorIs(t, err, auth.ErrInvalidCredentials)
}
