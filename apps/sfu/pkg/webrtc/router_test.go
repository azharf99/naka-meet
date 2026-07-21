package webrtc_test

import (
	"testing"

	"github.com/naka-meet/sfu/pkg/webrtc"
	pion "github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSFURouter_InitAndConfigurePorts(t *testing.T) {
	router, err := webrtc.NewSFURouter(50000, 50050)
	require.NoError(t, err)
	assert.NotNil(t, router)
}

func TestSFURouter_AddAndRemovePeer(t *testing.T) {
	router, err := webrtc.NewSFURouter(50000, 50050)
	require.NoError(t, err)

	peerID := "peer-1"
	pc, err := router.AddPeer(peerID)
	require.NoError(t, err)
	assert.NotNil(t, pc)
	assert.Equal(t, pion.PeerConnectionStateNew, pc.ConnectionState())

	// Remove Peer
	err = router.RemovePeer(peerID)
	require.NoError(t, err)
}

func TestSFURouter_RTPFanout(t *testing.T) {
	router, err := webrtc.NewSFURouter(50000, 50050)
	require.NoError(t, err)

	peer1ID := "peer-publisher"
	peer2ID := "peer-subscriber"

	_, err = router.AddPeer(peer1ID)
	require.NoError(t, err)

	_, err = router.AddPeer(peer2ID)
	require.NoError(t, err)

	// Create Mock Pion Track from publisher (VP8 video)
	mockTrack, err := pion.NewTrackLocalStaticSample(
		pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8},
		"video",
		"pion-stream",
	)
	require.NoError(t, err)

	// Broadcast track to all peers except publisher
	addedTracksCount, err := router.BroadcastTrack(peer1ID, mockTrack)
	require.NoError(t, err)
	assert.Equal(t, 1, addedTracksCount, "Should broadcast track to 1 subscriber")
}
