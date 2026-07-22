package webrtc_test

import (
	"testing"

	"github.com/naka-meet/sfu/pkg/webrtc"
	pion "github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSFURouter_MultiRoomTrackIsolation(t *testing.T) {
	router, err := webrtc.NewSFURouter(50000, 50050)
	require.NoError(t, err)

	// Create 3 peers
	peer1ID := "publisher-room-a"
	peer2ID := "subscriber-room-b"
	peer3ID := "subscriber-room-a"

	pc1, err := router.AddPeer(peer1ID)
	require.NoError(t, err)
	assert.NotNil(t, pc1)

	pc2, err := router.AddPeer(peer2ID)
	require.NoError(t, err)
	assert.NotNil(t, pc2)

	pc3, err := router.AddPeer(peer3ID)
	require.NoError(t, err)
	assert.NotNil(t, pc3)

	// Map peers to rooms
	router.SetPeerRoom(peer1ID, "room-a")
	router.SetPeerRoom(peer2ID, "room-b")
	router.SetPeerRoom(peer3ID, "room-a")

	// Create mock track from publisher
	mockTrack, err := pion.NewTrackLocalStaticSample(
		pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8},
		"video-1",
		"stream-1",
	)
	require.NoError(t, err)

	// Keep track of which peers received offers
	receivedOffers := make(map[string]bool)
	sendOfferCb := func(peerID, offerSDP string) {
		receivedOffers[peerID] = true
	}

	// Broadcast track in room-a
	router.BroadcastTrackAndRenegotiateWithMetadata("room-a", peer1ID, "Publisher", "camera", mockTrack, sendOfferCb)

	// Peer 3 (in room-a) should receive the track and offer (once peer connection signaling is stable)
	// Even if signaling state isn't stable or is mock, we can check if pc.GetTransceivers()/GetSenders() has tracks added
	assert.False(t, receivedOffers[peer2ID], "Peer in Room B should NOT receive offer for Room A track")
	
	// Check if track was actually added to PeerConnection's senders
	hasTrack2 := false
	for _, sender := range pc2.GetSenders() {
		if sender.Track() != nil && sender.Track().ID() == mockTrack.ID() {
			hasTrack2 = true
		}
	}
	assert.False(t, hasTrack2, "Peer in Room B should NOT have the track added to their PeerConnection")

	hasTrack3 := false
	for _, sender := range pc3.GetSenders() {
		if sender.Track() != nil && sender.Track().ID() == mockTrack.ID() {
			hasTrack3 = true
		}
	}
	// Note: We expect Peer 3 to have the track since it is in Room A
	assert.True(t, hasTrack3, "Peer in Room A should have the track added to their PeerConnection")
}
