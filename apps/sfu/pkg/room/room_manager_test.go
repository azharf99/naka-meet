package room_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/naka-meet/sfu/pkg/room"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoomManager_CreateAndAddParticipants(t *testing.T) {
	ctx := context.Background()
	rm := room.NewRoomManager(nil) // nil redis for in-memory unit tests

	roomSlug := "test-room-1"
	hostID, _ := uuid.NewV7()

	r, err := rm.CreateOrGetRoom(ctx, roomSlug, hostID.String())
	require.NoError(t, err)
	assert.Equal(t, roomSlug, r.Slug)
	assert.Equal(t, hostID.String(), r.HostID)

	// Add participant
	p1ID, _ := uuid.NewV7()
	p1 := &room.Participant{
		ID:       p1ID.String(),
		Name:     "Alice",
		JoinedAt: time.Now(),
	}

	err = rm.AddParticipant(ctx, roomSlug, p1)
	require.NoError(t, err)
	assert.Equal(t, 1, rm.GetParticipantCount(roomSlug))

	// Verify participant exists
	gotP, exists := rm.GetParticipant(roomSlug, p1ID.String())
	assert.True(t, exists)
	assert.Equal(t, "Alice", gotP.Name)
}

func TestRoomManager_HardLimit50Participants(t *testing.T) {
	ctx := context.Background()
	rm := room.NewRoomManager(nil)

	roomSlug := "full-room"
	hostID, _ := uuid.NewV7()

	_, err := rm.CreateOrGetRoom(ctx, roomSlug, hostID.String())
	require.NoError(t, err)

	// Fill room with 50 participants
	for i := 0; i < 50; i++ {
		pID, _ := uuid.NewV7()
		err := rm.AddParticipant(ctx, roomSlug, &room.Participant{
			ID:   pID.String(),
			Name: "User",
		})
		require.NoError(t, err)
	}

	assert.Equal(t, 50, rm.GetParticipantCount(roomSlug))

	// 51st participant should fail with ErrRoomFull
	p51ID, _ := uuid.NewV7()
	err = rm.AddParticipant(ctx, roomSlug, &room.Participant{
		ID:   p51ID.String(),
		Name: "Overflow",
	})
	assert.ErrorIs(t, err, room.ErrRoomFull)
}

func TestRoomManager_15SecGracefulDisconnect(t *testing.T) {
	ctx := context.Background()
	rm := room.NewRoomManager(nil)

	roomSlug := "reconnect-room"
	hostID, _ := uuid.NewV7()
	_, _ = rm.CreateOrGetRoom(ctx, roomSlug, hostID.String())

	pID, _ := uuid.NewV7()
	p := &room.Participant{
		ID:   pID.String(),
		Name: "Bob",
	}
	_ = rm.AddParticipant(ctx, roomSlug, p)

	// Trigger Disconnect with 50ms timeout (testing reconnect window quickly)
	reconnectWindow := 100 * time.Millisecond
	rm.HandleDisconnect(roomSlug, pID.String(), reconnectWindow)

	// Immediately check: participant is still in room (pending reconnect)
	_, exists := rm.GetParticipant(roomSlug, pID.String())
	assert.True(t, exists, "Participant should still exist during grace period")

	// Wait for grace period to expire
	time.Sleep(150 * time.Millisecond)

	// Now participant should be removed
	_, existsAfter := rm.GetParticipant(roomSlug, pID.String())
	assert.False(t, existsAfter, "Participant should be removed after grace period expires")
}

func TestRoomManager_CancelDisconnectOnReconnect(t *testing.T) {
	ctx := context.Background()
	rm := room.NewRoomManager(nil)

	roomSlug := "reconnect-cancel-room"
	hostID, _ := uuid.NewV7()
	_, _ = rm.CreateOrGetRoom(ctx, roomSlug, hostID.String())

	pID, _ := uuid.NewV7()
	p := &room.Participant{
		ID:   pID.String(),
		Name: "Charlie",
	}
	_ = rm.AddParticipant(ctx, roomSlug, p)

	// Trigger disconnect with 150ms window
	rm.HandleDisconnect(roomSlug, pID.String(), 150*time.Millisecond)

	// Cancel disconnect by reconnecting after 50ms
	time.Sleep(50 * time.Millisecond)
	reconnected := rm.HandleReconnect(roomSlug, pID.String())
	assert.True(t, reconnected)

	// Wait past initial expiration time
	time.Sleep(120 * time.Millisecond)

	// Participant should still be present
	_, exists := rm.GetParticipant(roomSlug, pID.String())
	assert.True(t, exists, "Participant should remain in room after successful reconnect")
}
