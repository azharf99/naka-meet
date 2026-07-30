package webrtc_test

import (
	"sync"
	"testing"
	"time"

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

func TestSFURouter_RoomTracksAutoSubscribe(t *testing.T) {
	router, err := webrtc.NewSFURouter(50000, 50050)
	require.NoError(t, err)

	roomSlug := "test-room"
	pubID := "publisher-1"
	subID := "subscriber-1"

	_, err = router.AddPeer(pubID)
	require.NoError(t, err)

	mockTrack, err := pion.NewTrackLocalStaticSample(
		pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8},
		"video-1",
		"stream-1",
	)
	require.NoError(t, err)

	// Add track to room
	router.AddTrackToRoom(roomSlug, pubID, mockTrack)

	// Subscribing new peer to room tracks
	_, err = router.AddPeer(subID)
	require.NoError(t, err)

	count, err := router.SubscribePeerToRoomTracks(roomSlug, subID, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "New subscriber should be subscribed to 1 active room track")
}

func TestSFURouter_RoomTracksSkipOwnTracksAndCleanup(t *testing.T) {
	router, err := webrtc.NewSFURouter(50000, 50050)
	require.NoError(t, err)

	roomSlug := "cleanup-room"
	pubID := "publisher-self"

	_, err = router.AddPeer(pubID)
	require.NoError(t, err)

	mockTrack, err := pion.NewTrackLocalStaticSample(
		pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8},
		"video-self",
		"stream-self",
	)
	require.NoError(t, err)

	// Add track with metadata
	router.AddTrackToRoomWithMetadata(roomSlug, pubID, "Publisher Self", "camera", mockTrack)

	// Subscribing publisher to own room tracks should yield 0 tracks added
	count, err := router.SubscribePeerToRoomTracks(roomSlug, pubID, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "Publisher should not subscribe to their own tracks")

	// Remove peer should clean up roomTracks
	err = router.RemovePeer(pubID)
	require.NoError(t, err)

	tracks := router.GetRoomTracks(roomSlug)
	assert.Len(t, tracks, 0, "Room tracks published by removed peer should be cleaned up")
}

// TestSFURouter_ConcurrentTrackPublishDoesNotDropRenegotiation reproduces the
// "host/participants can't see all videos" bug: a publisher's camera and mic
// tracks (or two publishers joining back-to-back) each trigger their own
// renegotiation to the same subscriber at nearly the same instant. Without
// per-peer serialization, the second CreateOffer/SetLocalDescription races
// the first, silently fails because the PeerConnection isn't stable yet, and
// is never retried - permanently starving the subscriber of that track.
func TestSFURouter_ConcurrentTrackPublishDoesNotDropRenegotiation(t *testing.T) {
	router, err := webrtc.NewSFURouter(50000, 50050)
	require.NoError(t, err)

	pubID := "publisher-concurrent"
	subID := "subscriber-concurrent"
	roomSlug := "concurrent-room"

	_, err = router.AddPeer(pubID)
	require.NoError(t, err)
	subPC, err := router.AddPeer(subID)
	require.NoError(t, err)
	router.SetPeerRoom(pubID, roomSlug)
	router.SetPeerRoom(subID, roomSlug)

	// Stands in for the subscriber's real browser: answers whatever offer
	// the SFU sends, so a blocked renegotiation attempt can proceed as soon
	// as it acquires its turn instead of waiting out the stable-state timeout.
	fakeClientPC, err := pion.NewAPI().NewPeerConnection(pion.Configuration{})
	require.NoError(t, err)
	defer fakeClientPC.Close()

	var cbMu sync.Mutex
	offersReceived := 0
	sendOfferCb := func(peerID, offerSDP string) {
		cbMu.Lock()
		defer cbMu.Unlock()
		offersReceived++

		require.NoError(t, fakeClientPC.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeOffer, SDP: offerSDP}))
		answer, err := fakeClientPC.CreateAnswer(nil)
		require.NoError(t, err)
		require.NoError(t, fakeClientPC.SetLocalDescription(answer))
		require.NoError(t, subPC.SetRemoteDescription(answer))
	}

	audioTrack, err := pion.NewTrackLocalStaticSample(pion.RTPCodecCapability{MimeType: pion.MimeTypeOpus}, "audio-1", "stream-concurrent")
	require.NoError(t, err)
	videoTrack, err := pion.NewTrackLocalStaticSample(pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8}, "video-1", "stream-concurrent")
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		router.BroadcastTrackAndRenegotiateWithMetadata(roomSlug, pubID, "Publisher", "camera", audioTrack, sendOfferCb)
	}()
	go func() {
		defer wg.Done()
		router.BroadcastTrackAndRenegotiateWithMetadata(roomSlug, pubID, "Publisher", "camera", videoTrack, sendOfferCb)
	}()
	wg.Wait()

	assert.Len(t, subPC.GetSenders(), 2, "Both concurrently-published tracks should reach the subscriber's PeerConnection")

	cbMu.Lock()
	defer cbMu.Unlock()
	assert.Equal(t, 2, offersReceived, "Both concurrent renegotiation attempts should deliver an offer, not silently drop one")
}

// TestSFURouter_RenegotiationDeferredUntilStableThenDelivered reproduces the
// other half of the "host can't see all participant videos" bug: previously,
// if a target peer's PeerConnection wasn't stable within a fixed 3s timeout,
// renegotiatePeer gave up permanently even though the corresponding AddTrack
// had already happened - the track would never be announced via SDP unless
// some unrelated future renegotiation happened to fire. This asserts the
// offer is deferred (not dropped) and delivered once the peer returns to
// stable, with no fixed timeout involved.
func TestSFURouter_RenegotiationDeferredUntilStableThenDelivered(t *testing.T) {
	router, err := webrtc.NewSFURouter(50000, 50050)
	require.NoError(t, err)

	pubID := "publisher-deferred"
	subID := "subscriber-deferred"
	roomSlug := "deferred-room"

	_, err = router.AddPeer(pubID)
	require.NoError(t, err)
	subPC, err := router.AddPeer(subID)
	require.NoError(t, err)
	router.SetPeerRoom(pubID, roomSlug)
	router.SetPeerRoom(subID, roomSlug)

	// Simulate an in-flight negotiation already underway on the subscriber's
	// PeerConnection (e.g. it just joined and its own offer hasn't been
	// answered yet) by driving it into HaveLocalOffer before the broadcast.
	_, err = subPC.AddTransceiverFromKind(pion.RTPCodecTypeAudio)
	require.NoError(t, err)
	inFlightOffer, err := subPC.CreateOffer(nil)
	require.NoError(t, err)
	require.NoError(t, subPC.SetLocalDescription(inFlightOffer))
	require.Equal(t, pion.SignalingStateHaveLocalOffer, subPC.SignalingState())

	fakeClientPC, err := pion.NewAPI().NewPeerConnection(pion.Configuration{})
	require.NoError(t, err)
	defer fakeClientPC.Close()

	var mu sync.Mutex
	offersReceived := 0
	sendOfferCb := func(peerID, offerSDP string) {
		mu.Lock()
		offersReceived++
		mu.Unlock()
	}

	videoTrack, err := pion.NewTrackLocalStaticSample(pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8}, "video-1", "stream-deferred")
	require.NoError(t, err)

	router.BroadcastTrackAndRenegotiateWithMetadata(roomSlug, pubID, "Publisher", "camera", videoTrack, sendOfferCb)

	mu.Lock()
	assert.Equal(t, 0, offersReceived, "renegotiation should be deferred, not dropped, while the subscriber PC isn't stable")
	mu.Unlock()

	// Resolve the in-flight negotiation by answering it, returning the
	// subscriber's PC to stable - this should trigger the deferred retry via
	// AddPeer's OnSignalingStateChange handler.
	require.NoError(t, fakeClientPC.SetRemoteDescription(inFlightOffer))
	answer, err := fakeClientPC.CreateAnswer(nil)
	require.NoError(t, err)
	require.NoError(t, fakeClientPC.SetLocalDescription(answer))
	require.NoError(t, subPC.SetRemoteDescription(answer))

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return offersReceived == 1
	}, 2*time.Second, 10*time.Millisecond, "deferred renegotiation should be delivered once the subscriber PC returns to stable")
}

// TestSFURouter_SubscribePeerToRoomTracksSkipsAlreadyBroadcastTrack
// reproduces the secondary defect from the join-race: if a track was already
// pushed onto a peer's PeerConnection via the live broadcast fan-out path,
// the pre-existing-tracks catch-up subscription must not add it again -
// doing so would create a second, duplicate sender for identical media.
func TestSFURouter_SubscribePeerToRoomTracksSkipsAlreadyBroadcastTrack(t *testing.T) {
	router, err := webrtc.NewSFURouter(50000, 50050)
	require.NoError(t, err)

	pubID := "publisher-dedup"
	subID := "subscriber-dedup"
	roomSlug := "dedup-room"

	_, err = router.AddPeer(pubID)
	require.NoError(t, err)
	subPC, err := router.AddPeer(subID)
	require.NoError(t, err)
	router.SetPeerRoom(pubID, roomSlug)
	router.SetPeerRoom(subID, roomSlug)

	mockTrack, err := pion.NewTrackLocalStaticSample(pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8}, "video-dedup", "stream-dedup")
	require.NoError(t, err)

	// Broadcast delivers the track directly onto the subscriber's PC (the
	// live fan-out path)...
	router.BroadcastTrackAndRenegotiateWithMetadata(roomSlug, pubID, "Publisher", "camera", mockTrack, func(string, string) {})
	require.Len(t, subPC.GetSenders(), 1)

	// ...so the catch-up subscription path for the same room must skip it
	// instead of adding a second, duplicate sender for identical media.
	count, err := router.SubscribePeerToRoomTracks(roomSlug, subID, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "already-broadcast track should not be subscribed again")
	assert.Len(t, subPC.GetSenders(), 1, "subscriber should not end up with a duplicate sender for the same track")
}


