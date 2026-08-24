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

// TestRoomManager_ReconnectSucceedsWhenRoomAtCapacity reproduces a bug where
// a participant reconnecting with the same ID (e.g. a brief network blip
// during the 15s HandleDisconnect grace window) was rejected with
// ErrRoomFull if the room happened to be at the 50-participant cap, turning
// a transient disconnect into a permanent, unrecoverable kick.
func TestRoomManager_ReconnectSucceedsWhenRoomAtCapacity(t *testing.T) {
	ctx := context.Background()
	rm := room.NewRoomManager(nil)

	roomSlug := "full-room-reconnect"
	hostID, _ := uuid.NewV7()
	_, err := rm.CreateOrGetRoom(ctx, roomSlug, hostID.String())
	require.NoError(t, err)

	var reconnectingID string
	for i := 0; i < 50; i++ {
		pID, _ := uuid.NewV7()
		reconnectingID = pID.String()
		require.NoError(t, rm.AddParticipant(ctx, roomSlug, &room.Participant{
			ID:   reconnectingID,
			Name: "User",
		}))
	}
	require.Equal(t, 50, rm.GetParticipantCount(roomSlug))

	// Re-add the same participant ID while the room is still at capacity -
	// this must succeed (it's a reconnect, not a new occupant).
	err = rm.AddParticipant(ctx, roomSlug, &room.Participant{
		ID:   reconnectingID,
		Name: "User",
	})
	assert.NoError(t, err)
	assert.Equal(t, 50, rm.GetParticipantCount(roomSlug), "reconnect should replace the existing entry, not grow the room")
}

// TestRoomManager_EgressExemptFromCapacity reproduces the egress recorder
// being counted against the human-facing 50-participant cap with no
// exemption - every recording session permanently reduced real headroom.
func TestRoomManager_EgressExemptFromCapacity(t *testing.T) {
	ctx := context.Background()
	rm := room.NewRoomManager(nil)

	roomSlug := "full-room-egress"
	hostID, _ := uuid.NewV7()
	_, err := rm.CreateOrGetRoom(ctx, roomSlug, hostID.String())
	require.NoError(t, err)

	for i := 0; i < 50; i++ {
		pID, _ := uuid.NewV7()
		require.NoError(t, rm.AddParticipant(ctx, roomSlug, &room.Participant{
			ID:   pID.String(),
			Name: "User",
		}))
	}

	egressID, _ := uuid.NewV7()
	err = rm.AddParticipant(ctx, roomSlug, &room.Participant{
		ID:       egressID.String(),
		Name:     "Egress Recorder",
		IsEgress: true,
	})
	assert.NoError(t, err, "egress recorder should be exempt from the human-facing participant cap")

	// A normal (non-egress) 51st participant must still be rejected.
	overflowID, _ := uuid.NewV7()
	err = rm.AddParticipant(ctx, roomSlug, &room.Participant{
		ID:   overflowID.String(),
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

func TestRoomManager_ScreenShare_LatestSharePresentedByDefault(t *testing.T) {
	ctx := context.Background()
	rm := room.NewRoomManager(nil)
	roomSlug := "screen-room"
	hostID, _ := uuid.NewV7()
	_, err := rm.CreateOrGetRoom(ctx, roomSlug, hostID.String())
	require.NoError(t, err)

	activeID, activeName, changed := rm.SetScreenSharing(roomSlug, "alice", "Alice", true)
	assert.Equal(t, "alice", activeID)
	assert.Equal(t, "Alice", activeName)
	assert.True(t, changed)

	// A second, later share must become active — "latest wins" — without
	// evicting Alice's from the room's set of sharing publishers (she's
	// suspended, not stopped).
	activeID, activeName, changed = rm.SetScreenSharing(roomSlug, "bob", "Bob", true)
	assert.Equal(t, "bob", activeID)
	assert.Equal(t, "Bob", activeName)
	assert.True(t, changed)

	gotID, gotName, ok := rm.GetActivePresentation(roomSlug)
	require.True(t, ok)
	assert.Equal(t, "bob", gotID)
	assert.Equal(t, "Bob", gotName)
}

func TestRoomManager_ScreenShare_StoppingActivePresenterClearsStage(t *testing.T) {
	ctx := context.Background()
	rm := room.NewRoomManager(nil)
	roomSlug := "screen-room"
	hostID, _ := uuid.NewV7()
	_, _ = rm.CreateOrGetRoom(ctx, roomSlug, hostID.String())

	rm.SetScreenSharing(roomSlug, "alice", "Alice", true)

	activeID, activeName, changed := rm.SetScreenSharing(roomSlug, "alice", "Alice", false)
	assert.Empty(t, activeID)
	assert.Empty(t, activeName)
	assert.True(t, changed)
}

func TestRoomManager_ScreenShare_StoppingASuspendedShareDoesNotClearStage(t *testing.T) {
	ctx := context.Background()
	rm := room.NewRoomManager(nil)
	roomSlug := "screen-room"
	hostID, _ := uuid.NewV7()
	_, _ = rm.CreateOrGetRoom(ctx, roomSlug, hostID.String())

	rm.SetScreenSharing(roomSlug, "alice", "Alice", true)
	rm.SetScreenSharing(roomSlug, "bob", "Bob", true) // bob is now active; alice suspended

	activeID, activeName, changed := rm.SetScreenSharing(roomSlug, "alice", "Alice", false)
	assert.Equal(t, "bob", activeID, "stopping a suspended share must not disturb who's actually on stage")
	assert.Equal(t, "Bob", activeName)
	assert.True(t, changed, "the sharing publisher set itself did change, even though the stage didn't")
}

func TestRoomManager_ScreenShare_HostCanReclaimOwnSuspendedShare(t *testing.T) {
	ctx := context.Background()
	rm := room.NewRoomManager(nil)
	roomSlug := "screen-room"
	hostID, _ := uuid.NewV7()
	_, _ = rm.CreateOrGetRoom(ctx, roomSlug, hostID.String())

	rm.SetScreenSharing(roomSlug, hostID.String(), "Host", true)
	rm.SetScreenSharing(roomSlug, "guest", "Guest", true) // suspends the host's share

	// Host reclaims — this is literally the same mechanism a host would use
	// to pick anyone else's active share; targeting their own ID is what
	// "take back the presentation" means here.
	name, ok := rm.SetActivePresentation(roomSlug, hostID.String())
	assert.True(t, ok)
	assert.Equal(t, "Host", name)

	gotID, _, _ := rm.GetActivePresentation(roomSlug)
	assert.Equal(t, hostID.String(), gotID)
}

func TestRoomManager_ScreenShare_SetActivePresentationRejectsNonPresentingTarget(t *testing.T) {
	ctx := context.Background()
	rm := room.NewRoomManager(nil)
	roomSlug := "screen-room"
	hostID, _ := uuid.NewV7()
	_, _ = rm.CreateOrGetRoom(ctx, roomSlug, hostID.String())

	rm.SetScreenSharing(roomSlug, "alice", "Alice", true)

	// "carol" was never in the sharing set (stale ID, or never shared at
	// all) — must be refused rather than putting an empty tile on stage.
	name, ok := rm.SetActivePresentation(roomSlug, "carol")
	assert.False(t, ok)
	assert.Empty(t, name)

	gotID, _, _ := rm.GetActivePresentation(roomSlug)
	assert.Equal(t, "alice", gotID, "the rejected request must not have disturbed the actual stage")
}
