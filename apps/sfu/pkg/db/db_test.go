package db_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/naka-meet/sfu/pkg/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModels_UUIDv7Fields(t *testing.T) {
	id, err := uuid.NewV7()
	require.NoError(t, err)

	user := db.User{
		ID:    id.String(),
		Name:  "Test User",
		Email: "user@example.com",
	}

	assert.Equal(t, id.String(), user.ID)
	assert.Equal(t, "user@example.com", user.Email)

	rec := db.Recording{
		ID:     id.String(),
		RoomID: "room-123",
		Status: "completed",
	}

	assert.Equal(t, "completed", rec.Status)
}
