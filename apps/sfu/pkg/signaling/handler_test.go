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


