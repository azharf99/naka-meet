package signaling_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/naka-meet/sfu/pkg/auth"
	"github.com/naka-meet/sfu/pkg/room"
	"github.com/naka-meet/sfu/pkg/signaling"
	"github.com/naka-meet/sfu/pkg/webrtc"
	pion "github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


// TestSignaling_ServerSendsPeriodicPing guards against a stalled/dead
// connection sitting registered forever with no liveness detection, which
// let a single slow client stall broadcastToRoom's write loop for everyone
// else in the room indefinitely.
func TestSignaling_ServerSendsPeriodicPing(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)
	// Keepalive intervals are per-Handler fields (not package globals), so
	// shrinking them here only affects connections made on this Handler
	// instance - no risk of racing another test's connections/goroutines.
	handler.SetKeepaliveIntervalsForTesting(50*time.Millisecond, 500*time.Millisecond)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	userA, _ := uuid.NewV7()
	tokenA, _ := auth.GenerateTokenWithName(userA.String(), "UserA", "host", secret, 1*time.Hour)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=ping-room&token=" + tokenA
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	pingReceived := make(chan struct{}, 1)
	ws.SetPingHandler(func(string) error {
		select {
		case pingReceived <- struct{}{}:
		default:
		}
		return ws.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second))
	})

	// Reads pump the control-frame handler; the server's ping arrives as a
	// control frame, not a data message, so ReadMessage just needs to run.
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()

	select {
	case <-pingReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("expected a ping frame from the server within the configured ping period")
	}
}

func TestSignaling_HTTPUpgradeUnauthorized(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=room-1"

	// Connect without JWT cookie or query token
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	assert.Error(t, err)
	if resp != nil {
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	}
}

// TestSignaling_HTTPUpgradeRejectsDisallowedOrigin guards the ALLOWED_ORIGINS
// allowlist: a well-formed, authenticated request from an origin outside the
// configured allowlist (e.g. a participant reaching the app via LAN IP or
// 127.0.0.1 while only http://localhost:3000 is allowlisted) must still be
// rejected at the WS-upgrade step, not silently admitted.
func TestSignaling_HTTPUpgradeRejectsDisallowedOrigin(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)
	handler.SetAllowedOrigins([]string{"http://localhost:3000"})

	userID, _ := uuid.NewV7()
	token, err := auth.GenerateToken(userID.String(), "host", secret, 1*time.Hour)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=room-1&token=" + token

	header := http.Header{}
	header.Add("Origin", "http://192.168.18.3:3000")

	_, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	assert.Error(t, err)
	if resp != nil {
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	}
}

func TestSignaling_HTTPUpgradeAuthorizedViaCookie(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	userID, _ := uuid.NewV7()
	token, err := auth.GenerateToken(userID.String(), "host", secret, 1*time.Hour)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=room-1"

	header := http.Header{}
	header.Add("Cookie", "jwt_token="+token)

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	require.NoError(t, err)
	defer ws.Close()

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
}

func TestSignaling_HTTPUpgradeAuthorizedViaQueryParam(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	userID, _ := uuid.NewV7()
	token, err := auth.GenerateToken(userID.String(), "participant", secret, 1*time.Hour)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=demo-room&token=" + token

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
}

func TestSignaling_HTTPUpgradeUsesDisplayName(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	userID, _ := uuid.NewV7()
	token, err := auth.GenerateTokenWithName(userID.String(), "Budi Ganteng", "participant", secret, 1*time.Hour)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=display-room&token=" + token

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	assert.Eventually(t, func() bool {
		p, found := rm.GetParticipant("display-room", userID.String())
		return found && p.Name == "Budi Ganteng"
	}, 1*time.Second, 10*time.Millisecond, "Participant should be added to RoomManager with correct display name")
}

func TestSignaling_TrackMetadataRelay(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	userID, _ := uuid.NewV7()
	token, err := auth.GenerateTokenWithName(userID.String(), "Presenter", "participant", secret, 1*time.Hour)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=br4-room&token=" + token

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	// Send out-of-band track metadata
	metaMsg := map[string]string{
		"type":      "track_metadata",
		"stream_id": "screen-stream-101",
		"kind":      "screen",
	}
	err = ws.WriteJSON(metaMsg)
	require.NoError(t, err)
}

func TestSignaling_SDPOfferAnswerExchange(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	userID, _ := uuid.NewV7()
	token, err := auth.GenerateTokenWithName(userID.String(), "Alice", "host", secret, 1*time.Hour)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=sdp-room&token=" + token
	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	// Create client PeerConnection to generate SDP offer
	clientAPI := pion.NewAPI()
	clientPC, err := clientAPI.NewPeerConnection(pion.Configuration{})
	require.NoError(t, err)
	defer clientPC.Close()

	// Add audio transceiver
	_, err = clientPC.AddTransceiverFromKind(pion.RTPCodecTypeAudio)
	require.NoError(t, err)

	offer, err := clientPC.CreateOffer(nil)
	require.NoError(t, err)
	require.NoError(t, clientPC.SetLocalDescription(offer))

	// Send SDP offer over WebSocket
	offerMsg := map[string]string{
		"type": "offer",
		"sdp":  offer.SDP,
	}
	require.NoError(t, ws.WriteJSON(offerMsg))

	// Expect SDP answer back from SFU handler. The very first message on the
	// wire isn't guaranteed to be the answer: pc.OnICECandidate on the server
	// side writes trickled "candidate" messages from its own goroutine,
	// independent of the message loop that writes "answer" - so it can land
	// before, after, or between them (see the sibling tests below that hit
	// this same flakiness). Loop past any candidates instead of assuming a
	// fixed position.
	msg := readUntilType(t, ws, "answer", 2*time.Second)
	require.NotNil(t, msg, "SFU should reply with SDP answer, possibly interleaved with trickled ICE candidates")
	assert.Equal(t, "answer", msg["type"])
	assert.NotEmpty(t, msg["sdp"])
}

func TestSignaling_RenegotiationOfferOnNewTrack(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	// Connect User A (Publisher)
	userA, _ := uuid.NewV7()
	tokenA, _ := auth.GenerateTokenWithName(userA.String(), "UserA", "host", secret, 1*time.Hour)
	wsURLA := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=reneg-room&token=" + tokenA
	wsA, _, err := websocket.DefaultDialer.Dial(wsURLA, nil)
	require.NoError(t, err)
	defer wsA.Close()

	// Connect User B (Subscriber)
	userB, _ := uuid.NewV7()
	tokenB, _ := auth.GenerateTokenWithName(userB.String(), "UserB", "participant", secret, 1*time.Hour)
	wsURLB := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=reneg-room&token=" + tokenB
	wsB, _, err := websocket.DefaultDialer.Dial(wsURLB, nil)
	require.NoError(t, err)
	defer wsB.Close()

	// User B must complete its own initial offer/answer handshake before it
	// becomes a valid broadcast target (see handler.go's AddPeer comment) —
	// a peer that never negotiates is never a candidate for renegotiation.
	clientAPI := pion.NewAPI()
	clientPC, err := clientAPI.NewPeerConnection(pion.Configuration{})
	require.NoError(t, err)
	defer clientPC.Close()
	_, err = clientPC.AddTransceiverFromKind(pion.RTPCodecTypeAudio)
	require.NoError(t, err)

	bOffer, err := clientPC.CreateOffer(nil)
	require.NoError(t, err)
	require.NoError(t, clientPC.SetLocalDescription(bOffer))

	require.NoError(t, wsB.WriteJSON(map[string]string{
		"type": "offer",
		"sdp":  bOffer.SDP,
	}))

	// The very first message on the wire isn't guaranteed to be the answer:
	// pc.OnICECandidate on the server side writes trickled "candidate"
	// messages from its own goroutine, independent of the message loop that
	// writes "answer" — so it can legitimately land before, after, or
	// between them. Loop past any candidates instead of assuming order.
	var ansMsg struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	}
	foundAnswer := false
	answerDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(answerDeadline) && !foundAnswer {
		require.NoError(t, wsB.SetReadDeadline(answerDeadline))
		require.NoError(t, wsB.ReadJSON(&ansMsg))
		foundAnswer = ansMsg.Type == "answer"
	}
	require.True(t, foundAnswer, "User B should receive its own initial SDP answer, possibly interleaved with trickled ICE candidates")
	require.NoError(t, clientPC.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: ansMsg.SDP}))

	// Create mock track and add to room
	mockTrack, err := pion.NewTrackLocalStaticSample(
		pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8},
		"video-a",
		"stream-a",
	)
	require.NoError(t, err)

	// Broadcast track & trigger renegotiation offer to User B
	handler.AddTrackAndRenegotiate("reneg-room", userA.String(), mockTrack)

	// User B should receive a renegotiation SDP offer over WebSocket, mixed
	// in with unrelated messages like trickled ICE candidates. A fixed
	// iteration count here is exactly the flakiness this test used to have
	// (see the answer-read loop above): however many candidates land before
	// the offer, loop on a deadline instead of a message-count guess.
	var msgB struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	}
	receivedOffer := false
	offerDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(offerDeadline) && !receivedOffer {
		if err := wsB.SetReadDeadline(offerDeadline); err != nil {
			break
		}
		if err := wsB.ReadJSON(&msgB); err != nil {
			break
		}
		if msgB.Type == "offer" {
			receivedOffer = true
		}
	}
	require.True(t, receivedOffer, "User B should receive renegotiation SDP offer from SFU")
	assert.NotEmpty(t, msgB.SDP)
}

// TestSignaling_JoinDuringConcurrentBroadcastDoesNotStallOwnOffer reproduces
// the "participants can't see host/other participants" bug: a peer that has
// connected but not yet sent its own join offer must never be treated as a
// valid broadcast target - otherwise a concurrent publisher's broadcast can
// push a server-initiated offer into it, corrupting its PeerConnection's
// signaling state so that its real join offer is later silently rejected.
func TestSignaling_JoinDuringConcurrentBroadcastDoesNotStallOwnOffer(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	// Publisher A already in the room.
	userA, _ := uuid.NewV7()
	tokenA, _ := auth.GenerateTokenWithName(userA.String(), "UserA", "host", secret, 1*time.Hour)
	wsURLA := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=race-room&token=" + tokenA
	wsA, _, err := websocket.DefaultDialer.Dial(wsURLA, nil)
	require.NoError(t, err)
	defer wsA.Close()

	// User B connects but has NOT sent its own offer yet.
	userB, _ := uuid.NewV7()
	tokenB, _ := auth.GenerateTokenWithName(userB.String(), "UserB", "participant", secret, 1*time.Hour)
	wsURLB := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=race-room&token=" + tokenB
	wsB, _, err := websocket.DefaultDialer.Dial(wsURLB, nil)
	require.NoError(t, err)
	defer wsB.Close()

	// Give the connection goroutine a moment to run past AddPeer/registerConn
	// (mirrors the real-world window between a peer connecting and the
	// browser's own offer arriving over the WS).
	time.Sleep(20 * time.Millisecond)

	mockTrack, err := pion.NewTrackLocalStaticSample(
		pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8},
		"video-race",
		"stream-race",
	)
	require.NoError(t, err)
	// Concurrent broadcast fired into the room while B still hasn't sent its
	// own offer - before the fix, this would corrupt B's PeerConnection
	// signaling state via an unsolicited server-initiated offer.
	handler.AddTrackAndRenegotiate("race-room", userA.String(), mockTrack)

	// Now B sends its own real join offer.
	clientAPI := pion.NewAPI()
	clientPC, err := clientAPI.NewPeerConnection(pion.Configuration{})
	require.NoError(t, err)
	defer clientPC.Close()
	_, err = clientPC.AddTransceiverFromKind(pion.RTPCodecTypeAudio)
	require.NoError(t, err)

	offer, err := clientPC.CreateOffer(nil)
	require.NoError(t, err)
	require.NoError(t, clientPC.SetLocalDescription(offer))

	require.NoError(t, wsB.WriteJSON(map[string]string{
		"type": "offer",
		"sdp":  offer.SDP,
	}))

	// Read past any unrelated messages (e.g. trickled ICE candidates) until
	// the answer to B's own offer arrives.
	var ansMsg struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	}
	receivedAnswer := false
	for i := 0; i < 5; i++ {
		require.NoError(t, wsB.SetReadDeadline(time.Now().Add(2*time.Second)))
		if err := wsB.ReadJSON(&ansMsg); err != nil {
			break
		}
		if ansMsg.Type == "answer" {
			receivedAnswer = true
			break
		}
	}
	require.True(t, receivedAnswer, "User B should still receive an answer to its own join offer despite the concurrent broadcast")
	assert.NotEmpty(t, ansMsg.SDP)
}

func TestSignaling_PreExistingTracksOfferAndMetadataOnJoin(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	// Connect User A and add a pre-existing track
	userA, _ := uuid.NewV7()
	tokenA, _ := auth.GenerateTokenWithName(userA.String(), "Host Alice", "host", secret, 1*time.Hour)
	wsURLA := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=pre-room&token=" + tokenA
	wsA, _, err := websocket.DefaultDialer.Dial(wsURLA, nil)
	require.NoError(t, err)
	defer wsA.Close()

	mockTrack, err := pion.NewTrackLocalStaticSample(
		pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8},
		"video-pre",
		"stream-pre",
	)
	require.NoError(t, err)
	handler.AddTrackAndRenegotiateWithMetadata("pre-room", userA.String(), "Host Alice", "camera", mockTrack)

	// Now User B joins and sends initial SDP offer
	userB, _ := uuid.NewV7()
	tokenB, _ := auth.GenerateTokenWithName(userB.String(), "Guest Bob", "participant", secret, 1*time.Hour)
	wsURLB := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=pre-room&token=" + tokenB
	wsB, _, err := websocket.DefaultDialer.Dial(wsURLB, nil)
	require.NoError(t, err)
	defer wsB.Close()

	clientAPI := pion.NewAPI()
	clientPC, err := clientAPI.NewPeerConnection(pion.Configuration{})
	require.NoError(t, err)
	defer clientPC.Close()
	_, err = clientPC.AddTransceiverFromKind(pion.RTPCodecTypeAudio)
	require.NoError(t, err)

	offer, err := clientPC.CreateOffer(nil)
	require.NoError(t, err)
	require.NoError(t, clientPC.SetLocalDescription(offer))

	require.NoError(t, wsB.WriteJSON(map[string]string{
		"type": "offer",
		"sdp":  offer.SDP,
	}))

	// User B should first receive the initial answer — but, as in
	// TestSignaling_RenegotiationOfferOnNewTrack above, trickled ICE
	// "candidate" messages from the server's own OnICECandidate goroutine
	// can legitimately land before it, so loop past any candidates rather
	// than assuming the very first message is the answer.
	var ansMsg struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	}
	foundAnswer := false
	answerDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(answerDeadline) && !foundAnswer {
		require.NoError(t, wsB.SetReadDeadline(answerDeadline))
		require.NoError(t, wsB.ReadJSON(&ansMsg))
		foundAnswer = ansMsg.Type == "answer"
	}
	assert.True(t, foundAnswer, "User B should receive its own initial SDP answer, possibly interleaved with trickled ICE candidates")

	// Then User B should receive track_metadata for Alice's pre-existing
	// track AND an SDP offer, in either order and however many trickled
	// candidate messages fall between them — so loop on a deadline until
	// both are seen instead of a fixed message count that assumes no
	// candidates interleave here either.
	var receivedOffer bool
	var receivedMetadata bool

	readDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(readDeadline) && (!receivedOffer || !receivedMetadata) {
		var msg struct {
			Type     string `json:"type"`
			SDP      string `json:"sdp"`
			StreamID string `json:"stream_id"`
			PeerName string `json:"peer_name"`
			Kind     string `json:"kind"`
		}
		if err := wsB.SetReadDeadline(readDeadline); err != nil {
			break
		}
		if err := wsB.ReadJSON(&msg); err != nil {
			break
		}
		if msg.Type == "offer" && msg.SDP != "" {
			receivedOffer = true
		} else if msg.Type == "track_metadata" && msg.StreamID == "stream-pre" {
			receivedMetadata = true
			assert.Equal(t, "Host Alice", msg.PeerName)
			assert.Equal(t, "camera", msg.Kind)
		}
	}

	assert.True(t, receivedOffer, "User B should receive renegotiation offer containing pre-existing room tracks after initial answer")
	assert.True(t, receivedMetadata, "User B should receive track_metadata with display name for pre-existing tracks")
}

func TestSignaling_ParticipantLeftBroadcastOnDisconnect(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	// Connect User A (Host)
	userA, _ := uuid.NewV7()
	tokenA, _ := auth.GenerateTokenWithName(userA.String(), "UserA", "host", secret, 1*time.Hour)
	wsURLA := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=disconnect-room&token=" + tokenA
	wsA, _, err := websocket.DefaultDialer.Dial(wsURLA, nil)
	require.NoError(t, err)
	defer wsA.Close()

	// Connect User B (Guest)
	userB, _ := uuid.NewV7()
	tokenB, _ := auth.GenerateTokenWithName(userB.String(), "UserB", "participant", secret, 1*time.Hour)
	wsURLB := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=disconnect-room&token=" + tokenB
	wsB, _, err := websocket.DefaultDialer.Dial(wsURLB, nil)
	require.NoError(t, err)

	// User B disconnects (closes WS connection)
	_ = wsB.Close()

	// User A should receive a participant_left message for User B
	var msgA struct {
		Type   string `json:"type"`
		PeerID string `json:"peer_id"`
	}
	require.NoError(t, wsA.SetReadDeadline(time.Now().Add(2*time.Second)))
	err = wsA.ReadJSON(&msgA)
	require.NoError(t, err, "User A should receive participant_left notification when User B disconnects")
	assert.Equal(t, "participant_left", msgA.Type)
	assert.Equal(t, userB.String(), msgA.PeerID)
}

// TestSignaling_RemoveParticipant_SendsRemovedMessageAndDisconnects covers
// the host-triggered "kick" path: the removed participant should get a
// distinct "removed" message (not just a raw connection drop it has to
// guess the reason for), everyone else in the room should see the usual
// participant_left tile-cleanup, and the removal should be immediate — not
// deferred behind the normal 15s reconnect-grace window.
func TestSignaling_RemoveParticipant_SendsRemovedMessageAndDisconnects(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	userA, _ := uuid.NewV7()
	tokenA, _ := auth.GenerateTokenWithName(userA.String(), "Host", "host", secret, 1*time.Hour)
	wsURLA := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=remove-room&token=" + tokenA
	wsA, _, err := websocket.DefaultDialer.Dial(wsURLA, nil)
	require.NoError(t, err)
	defer wsA.Close()

	userB, _ := uuid.NewV7()
	tokenB, _ := auth.GenerateTokenWithName(userB.String(), "Guest", "participant", secret, 1*time.Hour)
	wsURLB := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=remove-room&token=" + tokenB
	wsB, _, err := websocket.DefaultDialer.Dial(wsURLB, nil)
	require.NoError(t, err)
	defer wsB.Close()

	// The client's Dial() returns as soon as the HTTP upgrade completes,
	// which races the server's own post-upgrade setup (AddParticipant,
	// AddPeer, then registerConn — see ServeHTTP) — registerConn in
	// particular is what RemoveParticipant depends on, so poll rather than
	// assume it's already done by the time Dial() returns.
	var removed bool
	require.Eventually(t, func() bool {
		removed = handler.RemoveParticipant("remove-room", userB.String(), "host_removed")
		return removed
	}, 2*time.Second, 10*time.Millisecond, "RemoveParticipant should eventually succeed once User B's connection finishes registering")

	var removedMsg struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	}
	require.NoError(t, wsB.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, wsB.ReadJSON(&removedMsg))
	assert.Equal(t, "removed", removedMsg.Type)
	assert.Equal(t, "host_removed", removedMsg.Reason)

	var leftMsg struct {
		Type   string `json:"type"`
		PeerID string `json:"peer_id"`
	}
	require.NoError(t, wsA.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, wsA.ReadJSON(&leftMsg))
	assert.Equal(t, "participant_left", leftMsg.Type)
	assert.Equal(t, userB.String(), leftMsg.PeerID)

	// Immediate, not deferred behind the 15s reconnect grace: RemoveParticipant
	// removes the participant from the RoomManager itself before closing the
	// connection, so the count reflects the kick right away.
	assert.Equal(t, 1, rm.GetParticipantCount("remove-room"), "removed participant should no longer count toward the room")
}

// TestSignaling_RemoveParticipant_ScopedToRoom guards against a host
// targeting a participant ID that belongs to a different room than the one
// they have authority over — the API layer checks host-of-this-room, but
// RemoveParticipant itself must independently refuse to act cross-room in
// case that check is ever bypassed or misused.
func TestSignaling_RemoveParticipant_ScopedToRoom(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	userB, _ := uuid.NewV7()
	tokenB, _ := auth.GenerateTokenWithName(userB.String(), "Guest", "participant", secret, 1*time.Hour)
	wsURLB := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=real-room&token=" + tokenB
	wsB, _, err := websocket.DefaultDialer.Dial(wsURLB, nil)
	require.NoError(t, err)
	defer wsB.Close()

	removed := handler.RemoveParticipant("some-other-room", userB.String(), "host_removed")
	assert.False(t, removed, "RemoveParticipant must not act on a participant who isn't in the specified room")
	// AddParticipant for "real-room" races Dial() returning, same as the
	// registerConn race noted in the test above — poll rather than assume.
	require.Eventually(t, func() bool {
		return rm.GetParticipantCount("real-room") == 1
	}, 2*time.Second, 10*time.Millisecond, "participant should remain untouched in their actual room")
}

// TestSignaling_RemoveParticipant_ReturnsFalseWhenNotConnected covers the
// REST endpoint calling this for a stale/already-disconnected participant
// ID — it should report failure (surfaced as 404 by the API layer) rather
// than panicking or silently succeeding.
func TestSignaling_RemoveParticipant_ReturnsFalseWhenNotConnected(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	unknownID, _ := uuid.NewV7()
	removed := handler.RemoveParticipant("empty-room", unknownID.String(), "host_removed")
	assert.False(t, removed)
}

// TestSignaling_ForceMuteParticipant_SendsForceMuteMessage covers BR1: the
// target should receive a distinct "force_mute" message it can act on
// itself (disabling its own local audio track is something only the
// target's own browser can do).
func TestSignaling_ForceMuteParticipant_SendsForceMuteMessage(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	guestID, _ := uuid.NewV7()
	guestToken, _ := auth.GenerateTokenWithName(guestID.String(), "Guest", "participant", secret, 1*time.Hour)
	wsGuestURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=mute-room&token=" + guestToken
	wsGuest, _, err := websocket.DefaultDialer.Dial(wsGuestURL, nil)
	require.NoError(t, err)
	defer wsGuest.Close()

	var muted bool
	require.Eventually(t, func() bool {
		muted = handler.ForceMuteParticipant("mute-room", guestID.String())
		return muted
	}, 2*time.Second, 10*time.Millisecond, "ForceMuteParticipant should eventually succeed once the guest's connection finishes registering")

	msg := readUntilType(t, wsGuest, "force_mute", 2*time.Second)
	require.NotNil(t, msg, "the target should receive a force_mute message it can act on itself")
}

// TestSignaling_ForceMuteParticipant_ScopedToRoom mirrors
// TestSignaling_RemoveParticipant_ScopedToRoom: a host must never be able
// to force-mute a participant outside the room they have authority over.
func TestSignaling_ForceMuteParticipant_ScopedToRoom(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	guestID, _ := uuid.NewV7()
	guestToken, _ := auth.GenerateTokenWithName(guestID.String(), "Guest", "participant", secret, 1*time.Hour)
	wsGuestURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=real-mute-room&token=" + guestToken
	wsGuest, _, err := websocket.DefaultDialer.Dial(wsGuestURL, nil)
	require.NoError(t, err)
	defer wsGuest.Close()

	muted := handler.ForceMuteParticipant("some-other-room", guestID.String())
	assert.False(t, muted, "ForceMuteParticipant must not act on a participant who isn't in the specified room")
}

func TestSignaling_ForceMuteParticipant_ReturnsFalseWhenNotConnected(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	unknownID, _ := uuid.NewV7()
	muted := handler.ForceMuteParticipant("empty-room", unknownID.String())
	assert.False(t, muted)
}

// TestSignaling_EgressRoleParticipantExemptFromRoomCap reproduces the egress
// recorder occupying a real slot against the human-facing 50-participant
// cap: every recording session permanently reduced real headroom in the
// room. An "egress"-role token must be exempt; a normal role must not be.
func TestSignaling_EgressRoleParticipantExemptFromRoomCap(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	roomSlug := "cap-room"
	var conns []*websocket.Conn
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	for i := 0; i < 50; i++ {
		uid, _ := uuid.NewV7()
		token, _ := auth.GenerateTokenWithName(uid.String(), "User", "participant", secret, 1*time.Hour)
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=" + roomSlug + "&token=" + token
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		conns = append(conns, ws)
	}
	require.Eventually(t, func() bool {
		return rm.GetParticipantCount(roomSlug) == 50
	}, 2*time.Second, 10*time.Millisecond)

	// A normal 51st participant must still be rejected. The WS upgrade
	// itself succeeds (it happens before the room-capacity check), but the
	// server immediately closes the connection without adding it to the
	// room, so it must never be reflected in the participant count.
	overflowID, _ := uuid.NewV7()
	overflowToken, _ := auth.GenerateTokenWithName(overflowID.String(), "Overflow", "participant", secret, 1*time.Hour)
	overflowURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=" + roomSlug + "&token=" + overflowToken
	overflowWs, _, err := websocket.DefaultDialer.Dial(overflowURL, nil)
	require.NoError(t, err)
	defer overflowWs.Close()
	_, _, readErr := overflowWs.ReadMessage()
	assert.Error(t, readErr, "a 51st non-egress participant's connection should be closed immediately by the server")
	assert.Equal(t, 50, rm.GetParticipantCount(roomSlug), "a rejected 51st participant must not be counted in the room")

	// An "egress"-role 51st connection must succeed and be reflected in the
	// participant count (it's exempt from the cap, not invisible to it).
	egressID, _ := uuid.NewV7()
	egressToken, _ := auth.GenerateTokenWithName(egressID.String(), "Egress Recorder", "egress", secret, 1*time.Hour)
	egressURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=" + roomSlug + "&token=" + egressToken
	egressWs, _, err := websocket.DefaultDialer.Dial(egressURL, nil)
	require.NoError(t, err, "an egress-role participant should be exempt from the room cap")
	defer egressWs.Close()
	require.Eventually(t, func() bool {
		return rm.GetParticipantCount(roomSlug) == 51
	}, 2*time.Second, 10*time.Millisecond, "egress participant should be added despite the room already being at the human-facing cap")
}

// readUntilType reads messages off ws until one decodes with the given
// "type" field or the deadline elapses, returning it as a raw map. Used
// throughout the screen-share arbitration tests below so an unrelated
// message (a trickled ICE candidate, a ping) landing between the message
// under test and the read doesn't make the test flaky — the same lesson
// learned the hard way in the renegotiation tests above.
func readUntilType(t *testing.T, ws *websocket.Conn, wantType string, timeout time.Duration) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		require.NoError(t, ws.SetReadDeadline(deadline))
		var msg map[string]interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			return nil
		}
		if msg["type"] == wantType {
			return msg
		}
	}
	return nil
}

// waitForConnRegistered gives a just-dialed connection's server-side
// goroutine a moment to run past AddPeer/registerConn before it's relied
// on to observe a broadcast — mirrors the identical wait used above in
// TestSignaling_JoinDuringConcurrentBroadcastDoesNotStallOwnOffer. Without
// it, Dial() returning (as soon as the HTTP upgrade completes) races the
// server's own post-upgrade setup, and a broadcast fired immediately after
// dialing can land before this connection is registered to receive it.
func waitForConnRegistered() {
	time.Sleep(20 * time.Millisecond)
}

// TestSignaling_ScreenShareArbitration_LatestSharePresentedAndBroadcast
// covers BR: when multiple participants share simultaneously, the latest
// one is what the room actually sees by default, and everyone is told via
// presentation_state — not just whoever happens to render first.
func TestSignaling_ScreenShareArbitration_LatestSharePresentedAndBroadcast(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	// Connecting first makes this the room's recorded host (see
	// ServeHTTP's CreateOrGetRoom call) — irrelevant to this test's
	// "latest wins" assertion, but matches how every other test in this
	// file establishes host authority.
	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host", "host", secret, 1*time.Hour)
	wsHostURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=arb-room&token=" + hostToken
	wsHost, _, err := websocket.DefaultDialer.Dial(wsHostURL, nil)
	require.NoError(t, err)
	defer wsHost.Close()

	observerID, _ := uuid.NewV7()
	observerToken, _ := auth.GenerateTokenWithName(observerID.String(), "Observer", "participant", secret, 1*time.Hour)
	wsObsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=arb-room&token=" + observerToken
	wsObs, _, err := websocket.DefaultDialer.Dial(wsObsURL, nil)
	require.NoError(t, err)
	defer wsObs.Close()
	waitForConnRegistered()

	require.NoError(t, wsHost.WriteJSON(map[string]string{"type": "track_metadata", "stream_id": "host-screen", "kind": "screen"}))
	msg := readUntilType(t, wsObs, "presentation_state", 2*time.Second)
	require.NotNil(t, msg, "observer should see the host's share become the active presentation")
	assert.Equal(t, hostID.String(), msg["active_peer_id"])
	assert.Equal(t, "Host", msg["active_peer_name"])

	guestID, _ := uuid.NewV7()
	guestToken, _ := auth.GenerateTokenWithName(guestID.String(), "Guest", "participant", secret, 1*time.Hour)
	wsGuestURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=arb-room&token=" + guestToken
	wsGuest, _, err := websocket.DefaultDialer.Dial(wsGuestURL, nil)
	require.NoError(t, err)
	defer wsGuest.Close()
	waitForConnRegistered()

	require.NoError(t, wsGuest.WriteJSON(map[string]string{"type": "track_metadata", "stream_id": "guest-screen", "kind": "screen"}))
	msg = readUntilType(t, wsObs, "presentation_state", 2*time.Second)
	require.NotNil(t, msg, "the guest's later share should take over the stage")
	assert.Equal(t, guestID.String(), msg["active_peer_id"])
	assert.Equal(t, "Guest", msg["active_peer_name"])
}

// TestSignaling_SetPresentation_HostCanReclaimSuspendedShare covers BR:
// when a participant's share suspends the host's own, the host can take it
// back — the same set_presentation mechanism as picking anyone else's
// share, just targeting their own ID.
func TestSignaling_SetPresentation_HostCanReclaimSuspendedShare(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host", "host", secret, 1*time.Hour)
	wsHostURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=reclaim-room&token=" + hostToken
	wsHost, _, err := websocket.DefaultDialer.Dial(wsHostURL, nil)
	require.NoError(t, err)
	defer wsHost.Close()

	guestID, _ := uuid.NewV7()
	guestToken, _ := auth.GenerateTokenWithName(guestID.String(), "Guest", "participant", secret, 1*time.Hour)
	wsGuestURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=reclaim-room&token=" + guestToken
	wsGuest, _, err := websocket.DefaultDialer.Dial(wsGuestURL, nil)
	require.NoError(t, err)
	defer wsGuest.Close()
	waitForConnRegistered()

	require.NoError(t, wsHost.WriteJSON(map[string]string{"type": "track_metadata", "stream_id": "host-screen", "kind": "screen"}))
	require.NotNil(t, readUntilType(t, wsGuest, "presentation_state", 2*time.Second))
	// wsHost also receives its own broadcast (server-triggered, no sender
	// exclusion — matches BroadcastRecordingState's pattern) — drain it so
	// the next read below can't pick up this stale copy instead of the
	// guest's actual share.
	require.NotNil(t, readUntilType(t, wsHost, "presentation_state", 2*time.Second))

	require.NoError(t, wsGuest.WriteJSON(map[string]string{"type": "track_metadata", "stream_id": "guest-screen", "kind": "screen"}))
	msg := readUntilType(t, wsHost, "presentation_state", 2*time.Second)
	require.NotNil(t, msg)
	require.Equal(t, guestID.String(), msg["active_peer_id"], "guest's share should have suspended the host's")

	// Drain guest's own copy of its share broadcast, same reason as above —
	// otherwise the reclaim read below could pick up this stale copy.
	require.NotNil(t, readUntilType(t, wsGuest, "presentation_state", 2*time.Second))

	require.NoError(t, wsHost.WriteJSON(map[string]interface{}{"type": "set_presentation", "peer_id": hostID.String()}))
	msg = readUntilType(t, wsGuest, "presentation_state", 2*time.Second)
	require.NotNil(t, msg, "guest should see the host reclaim the stage")
	assert.Equal(t, hostID.String(), msg["active_peer_id"])
	assert.Equal(t, "Host", msg["active_peer_name"])
}

// TestSignaling_SetPresentation_NonHostRejected guards the host-only gate:
// a guest's JWT (even one it could self-declare role:"host" on, which
// GetRoomHostID's actual-ownership check exists to defeat) must never be
// able to change the room's active presentation.
func TestSignaling_SetPresentation_NonHostRejected(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host", "host", secret, 1*time.Hour)
	wsHostURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=nonhost-room&token=" + hostToken
	wsHost, _, err := websocket.DefaultDialer.Dial(wsHostURL, nil)
	require.NoError(t, err)
	defer wsHost.Close()

	guestID, _ := uuid.NewV7()
	guestToken, _ := auth.GenerateTokenWithName(guestID.String(), "Guest", "participant", secret, 1*time.Hour)
	wsGuestURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=nonhost-room&token=" + guestToken
	wsGuest, _, err := websocket.DefaultDialer.Dial(wsGuestURL, nil)
	require.NoError(t, err)
	defer wsGuest.Close()
	waitForConnRegistered()

	require.NoError(t, wsHost.WriteJSON(map[string]string{"type": "track_metadata", "stream_id": "host-screen", "kind": "screen"}))
	require.NotNil(t, readUntilType(t, wsGuest, "presentation_state", 2*time.Second))
	// wsHost's own copy of that same broadcast — drain it, or the read
	// below would immediately match this stale message instead of proving
	// no new one arrived after the guest's rejected attempt.
	require.NotNil(t, readUntilType(t, wsHost, "presentation_state", 2*time.Second))

	require.NoError(t, wsGuest.WriteJSON(map[string]interface{}{"type": "set_presentation", "peer_id": guestID.String()}))

	msg := readUntilType(t, wsHost, "presentation_state", 400*time.Millisecond)
	assert.Nil(t, msg, "a non-host's set_presentation must be silently rejected, not change who's on stage")

	gotID, _, _ := rm.GetActivePresentation("nonhost-room")
	assert.Equal(t, hostID.String(), gotID, "the stage must remain unchanged")
}

// TestSignaling_SetPresentation_RejectsNonPresentingTarget guards against
// the host targeting a stale or bogus peer_id — must not put an empty
// tile on stage for everyone.
func TestSignaling_SetPresentation_RejectsNonPresentingTarget(t *testing.T) {
	secret := []byte("secret-key")
	rm := room.NewRoomManager(nil)
	router, _ := webrtc.NewSFURouter(50000, 50050)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host", "host", secret, 1*time.Hour)
	wsHostURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=stale-target-room&token=" + hostToken
	wsHost, _, err := websocket.DefaultDialer.Dial(wsHostURL, nil)
	require.NoError(t, err)
	defer wsHost.Close()

	require.NoError(t, wsHost.WriteJSON(map[string]string{"type": "track_metadata", "stream_id": "host-screen", "kind": "screen"}))

	require.Eventually(t, func() bool {
		id, _, _ := rm.GetActivePresentation("stale-target-room")
		return id == hostID.String()
	}, 2*time.Second, 10*time.Millisecond)

	bogusID, _ := uuid.NewV7()
	require.NoError(t, wsHost.WriteJSON(map[string]interface{}{"type": "set_presentation", "peer_id": bogusID.String()}))

	// Nothing to observe it on (host is the sole connection and would be
	// excluded from its own broadcast anyway), so assert against the
	// authoritative state directly: it must not have moved.
	time.Sleep(200 * time.Millisecond)
	gotID, _, _ := rm.GetActivePresentation("stale-target-room")
	assert.Equal(t, hostID.String(), gotID, "targeting a non-presenting peer must be refused, not clear the stage")
}






