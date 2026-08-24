//go:build integration

// Package signaling_test's integration_test.go is the L2 layer described in
// TESTING_STRATEGY.md §5 ("Integration & End-to-End Testing"): every other
// test in this package constructs room.NewRoomManager(nil), which skips all
// Redis code entirely (see RoomManager.AddParticipant's "if rm.rdb != nil"
// guard) — so the unit-test layer, however thorough, can never catch a
// regression in the actual Redis wiring (a bad HSet call, a connection that
// silently fails, a key/field naming mismatch) because it never exercises
// that path at all. These tests spin up a real Redis via Testcontainers and
// drive the exact same live /ws/signaling endpoint main.go wires up, so a
// break here means the deployed stack is actually broken, not just a mock.
//
// Gated behind the "integration" build tag (not part of a plain
// `go test ./...`) because it needs a working Docker daemon — CI invokes it
// explicitly (see .github/workflows/ci-cd.yml's "Integration tests" step).
// Skips itself with a clear message if Docker isn't reachable, so a local
// `go test -tags=integration ./...` on a machine without Docker running
// fails loud and explicable instead of hanging on a container pull.
package signaling_test

import (
	"context"
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
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// newIntegrationRedis starts a real Redis 7 container and returns a
// connected *redis.Client, skipping the test (not failing it) if Docker
// itself isn't reachable — this class of test is opt-in infrastructure, not
// something that should red the whole CI run just because a contributor's
// laptop doesn't have Docker running.
func newIntegrationRedis(t *testing.T) *redis.Client {
	t.Helper()
	ctx := context.Background()

	c, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("skipping integration test: could not start Redis container (is Docker running?): %v", err)
	}
	t.Cleanup(func() {
		_ = c.Terminate(context.Background())
	})

	connStr, err := c.ConnectionString(ctx)
	require.NoError(t, err)

	opts, err := redis.ParseURL(connStr)
	require.NoError(t, err)

	rdb := redis.NewClient(opts)
	require.NoError(t, rdb.Ping(ctx).Err())
	t.Cleanup(func() { _ = rdb.Close() })

	return rdb
}

// TestIntegration_RealRedis_SDPOfferAnswerAndParticipantMirroring is
// TESTING_STRATEGY.md §5's "light E2E flow" almost verbatim: real Redis
// container up, SFU initialized against it, a real SDP offer sent to the
// live signaling endpoint, and — the part no nil-Redis unit test can
// assert — the participant actually landed in Redis's
// room:{slug}:participants hash, proving RoomManager.AddParticipant's Redis
// branch genuinely round-trips against a real server, not just that the
// nil-check is correct.
func TestIntegration_RealRedis_SDPOfferAnswerAndParticipantMirroring(t *testing.T) {
	rdb := newIntegrationRedis(t)
	ctx := context.Background()

	secret := []byte("integration-secret-key")
	rm := room.NewRoomManager(rdb)
	router, err := webrtc.NewSFURouter(50000, 50050)
	require.NoError(t, err)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	userID, _ := uuid.NewV7()
	token, err := auth.GenerateTokenWithName(userID.String(), "Integration Alice", "host", secret, time.Hour)
	require.NoError(t, err)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=integration-room&token=" + token
	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	clientAPI := pion.NewAPI()
	clientPC, err := clientAPI.NewPeerConnection(pion.Configuration{})
	require.NoError(t, err)
	defer clientPC.Close()
	_, err = clientPC.AddTransceiverFromKind(pion.RTPCodecTypeAudio)
	require.NoError(t, err)

	offer, err := clientPC.CreateOffer(nil)
	require.NoError(t, err)
	require.NoError(t, clientPC.SetLocalDescription(offer))

	require.NoError(t, ws.WriteJSON(map[string]string{"type": "offer", "sdp": offer.SDP}))

	msg := readUntilTypeIntegration(t, ws, "answer", 5*time.Second)
	require.NotNil(t, msg, "SFU should reply with a real SDP answer over the live endpoint, backed by a real Redis")
	assert.Equal(t, "answer", msg["type"])
	assert.NotEmpty(t, msg["sdp"])

	// The real assertion this whole test exists for: did AddParticipant's
	// Redis branch actually write through to a real server.
	fields, err := rdb.HGetAll(ctx, "room:integration-room:participants").Result()
	require.NoError(t, err)
	assert.Equal(t, "Integration Alice", fields[userID.String()], "participant join should mirror to Redis, not just in-memory RoomManager state")
}

// TestIntegration_RealRedis_TwoPeersTrackAndMetadataExchange extends the
// above to two participants — TESTING_STRATEGY.md §5's "extend to two
// simulated peers exchanging tracks" — over the same real-Redis-backed
// stack, so the fan-out/renegotiation path this project has repeatedly hit
// regressions in (see PROJECT_STATE.md) is proven to still work end-to-end
// with genuine Redis mirroring in the loop, not just with rm := nil.
func TestIntegration_RealRedis_TwoPeersTrackAndMetadataExchange(t *testing.T) {
	rdb := newIntegrationRedis(t)
	ctx := context.Background()

	secret := []byte("integration-secret-key")
	rm := room.NewRoomManager(rdb)
	router, err := webrtc.NewSFURouter(50000, 50050)
	require.NoError(t, err)
	handler := signaling.NewHandler(rm, router, secret)

	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()

	// Peer A (host) joins and completes its own SDP offer/answer first.
	userA, _ := uuid.NewV7()
	tokenA, _ := auth.GenerateTokenWithName(userA.String(), "Host Alice", "host", secret, time.Hour)
	wsURLA := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=integration-2peer&token=" + tokenA
	wsA, _, err := websocket.DefaultDialer.Dial(wsURLA, nil)
	require.NoError(t, err)
	defer wsA.Close()

	clientAPI := pion.NewAPI()
	pcA, err := clientAPI.NewPeerConnection(pion.Configuration{})
	require.NoError(t, err)
	defer pcA.Close()
	_, err = pcA.AddTransceiverFromKind(pion.RTPCodecTypeAudio)
	require.NoError(t, err)
	offerA, err := pcA.CreateOffer(nil)
	require.NoError(t, err)
	require.NoError(t, pcA.SetLocalDescription(offerA))
	require.NoError(t, wsA.WriteJSON(map[string]string{"type": "offer", "sdp": offerA.SDP}))
	require.NotNil(t, readUntilTypeIntegration(t, wsA, "answer", 5*time.Second), "Alice should receive her own SDP answer")

	// Alice publishes a track into the room, exactly as her real client
	// would after getUserMedia + renegotiation.
	mockTrack, err := pion.NewTrackLocalStaticSample(
		pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8},
		"video-integration",
		"stream-integration",
	)
	require.NoError(t, err)
	handler.AddTrackAndRenegotiateWithMetadata("integration-2peer", userA.String(), "Host Alice", "camera", mockTrack)

	// Peer B (guest) joins after — should receive its own answer, plus a
	// renegotiation offer and track_metadata for Alice's pre-existing track.
	userB, _ := uuid.NewV7()
	tokenB, _ := auth.GenerateTokenWithName(userB.String(), "Guest Bob", "guest", secret, time.Hour)
	wsURLB := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/signaling?room_slug=integration-2peer&token=" + tokenB
	wsB, _, err := websocket.DefaultDialer.Dial(wsURLB, nil)
	require.NoError(t, err)
	defer wsB.Close()

	pcB, err := clientAPI.NewPeerConnection(pion.Configuration{})
	require.NoError(t, err)
	defer pcB.Close()
	_, err = pcB.AddTransceiverFromKind(pion.RTPCodecTypeAudio)
	require.NoError(t, err)
	offerB, err := pcB.CreateOffer(nil)
	require.NoError(t, err)
	require.NoError(t, pcB.SetLocalDescription(offerB))
	require.NoError(t, wsB.WriteJSON(map[string]string{"type": "offer", "sdp": offerB.SDP}))
	require.NotNil(t, readUntilTypeIntegration(t, wsB, "answer", 5*time.Second), "Bob should receive his own SDP answer")

	var receivedOffer, receivedMetadata bool
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (!receivedOffer || !receivedMetadata) {
		var msg struct {
			Type     string `json:"type"`
			SDP      string `json:"sdp"`
			StreamID string `json:"stream_id"`
			PeerName string `json:"peer_name"`
			Kind     string `json:"kind"`
		}
		if err := wsB.SetReadDeadline(deadline); err != nil {
			break
		}
		if err := wsB.ReadJSON(&msg); err != nil {
			break
		}
		if msg.Type == "offer" && msg.SDP != "" {
			receivedOffer = true
		} else if msg.Type == "track_metadata" && msg.StreamID == "stream-integration" {
			receivedMetadata = true
			assert.Equal(t, "Host Alice", msg.PeerName)
			assert.Equal(t, "camera", msg.Kind)
		}
	}
	assert.True(t, receivedOffer, "Bob should receive a renegotiation offer carrying Alice's pre-existing track")
	assert.True(t, receivedMetadata, "Bob should receive track_metadata identifying Alice's track")

	// Both joins should be mirrored in the real Redis hash — the two-peer
	// equivalent of the single-peer assertion above.
	fields, err := rdb.HGetAll(ctx, "room:integration-2peer:participants").Result()
	require.NoError(t, err)
	assert.Equal(t, "Host Alice", fields[userA.String()])
	assert.Equal(t, "Guest Bob", fields[userB.String()])
}

// readUntilTypeIntegration mirrors the readUntilType helper used throughout
// handler_test.go: the server's own OnICECandidate goroutine can interleave
// trickled "candidate" messages before/after/between the message a test is
// actually waiting for, so tests must loop past non-matching types on a
// deadline instead of assuming a fixed message position (see
// TestSignaling_SDPOfferAnswerExchange's comment on the same flakiness).
// Duplicated here (rather than reusing the unexported helper from
// handler_test.go) because this file only compiles under the "integration"
// build tag and the two must not depend on each other's build-tag state.
func readUntilTypeIntegration(t *testing.T, ws *websocket.Conn, wantType string, timeout time.Duration) map[string]string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var msg map[string]string
		if err := ws.SetReadDeadline(deadline); err != nil {
			return nil
		}
		if err := ws.ReadJSON(&msg); err != nil {
			return nil
		}
		if msg["type"] == wantType {
			return msg
		}
	}
	return nil
}
