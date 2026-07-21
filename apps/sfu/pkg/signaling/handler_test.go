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

	// Expect SDP answer back from SFU handler
	var ansMsg struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	}
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(2*time.Second)))
	err = ws.ReadJSON(&ansMsg)
	require.NoError(t, err, "SFU should reply with SDP answer")
	assert.Equal(t, "answer", ansMsg.Type)
	assert.NotEmpty(t, ansMsg.SDP)
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

	// Create mock track and add to room
	mockTrack, err := pion.NewTrackLocalStaticSample(
		pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8},
		"video-a",
		"stream-a",
	)
	require.NoError(t, err)

	// Broadcast track & trigger renegotiation offer to User B
	handler.AddTrackAndRenegotiate("reneg-room", userA.String(), mockTrack)

	// User B should receive renegotiation SDP offer over WebSocket
	var msgB struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	}
	require.NoError(t, wsB.SetReadDeadline(time.Now().Add(2*time.Second)))
	err = wsB.ReadJSON(&msgB)
	require.NoError(t, err, "User B should receive renegotiation SDP offer from SFU")
	assert.Equal(t, "offer", msgB.Type)
	assert.NotEmpty(t, msgB.SDP)
}





