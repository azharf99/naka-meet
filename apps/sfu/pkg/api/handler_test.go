package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/naka-meet/sfu/pkg/api"
	"github.com/naka-meet/sfu/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockPublisher struct {
	PublishedChannel string
	PublishedMessage string
}

func (m *MockPublisher) Publish(ctx context.Context, channel string, message interface{}) error {
	m.PublishedChannel = channel
	if str, ok := message.(string); ok {
		m.PublishedMessage = str
	}
	return nil
}

func TestAPI_LoginHandler(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)

	reqBody := `{"name":"Alice","role":"host"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Check cookie
	cookies := resp.Cookies()
	var jwtCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "jwt_token" {
			jwtCookie = c
			break
		}
	}
	require.NotNil(t, jwtCookie, "jwt_token cookie should be set")

	// Validate token
	claims, err := auth.ValidateToken(jwtCookie.Value, secret)
	require.NoError(t, err)
	assert.Equal(t, "host", claims.Role)
	assert.NotEmpty(t, claims.UserID)
}

func TestAPI_EgressTriggerHandler_DynamicSlug(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	handler := api.NewAPIHandler(secret, mockPub)

	// Host Token
	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateToken(hostID.String(), "host", secret, 1*time.Hour)

	// Trigger Egress for "demo-room" with START_RECORDING
	reqBody := `{"action":"START_RECORDING"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/live", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, "channel:egress_commands", mockPub.PublishedChannel)

	var payload map[string]string
	err := json.Unmarshal([]byte(mockPub.PublishedMessage), &payload)
	require.NoError(t, err)
	assert.Equal(t, "START_RECORDING", payload["action"])
	assert.Equal(t, "demo-room", payload["room"])
}

func TestAPI_EgressTriggerHandler_StopEgress(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	handler := api.NewAPIHandler(secret, mockPub)

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateToken(hostID.String(), "host", secret, 1*time.Hour)

	// Stop Egress for "demo-room"
	reqBody := `{"action":"STOP_EGRESS"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/live", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var payload map[string]string
	_ = json.Unmarshal([]byte(mockPub.PublishedMessage), &payload)
	assert.Equal(t, "STOP_EGRESS", payload["action"])
	assert.Equal(t, "demo-room", payload["room"])
}

func TestAPI_LoginHandler_GuestWithName(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)

	reqBody := `{"name":"Budi","role":"guest"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	err := json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "guest", body["role"])
	assert.Equal(t, "Budi", body["name"])
	assert.NotEmpty(t, body["user_id"])
	assert.NotEmpty(t, body["token"])

	cookies := resp.Cookies()
	var jwtCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "jwt_token" {
			jwtCookie = c
			break
		}
	}
	require.NotNil(t, jwtCookie)

	claims, err := auth.ValidateToken(jwtCookie.Value, secret)
	require.NoError(t, err)
	assert.Equal(t, "guest", claims.Role)
	assert.Equal(t, "Budi", claims.Name)
}

func TestAPI_CreateRoomHandler_HostAllowed(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, 1*time.Hour)

	reqBody := `{"slug":"my-team-meeting"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var respBody struct {
		Status string `json:"status"`
		Room   struct {
			ID     string `json:"id"`
			Slug   string `json:"slug"`
			HostID string `json:"host_id"`
		} `json:"room"`
	}
	err := json.NewDecoder(w.Body).Decode(&respBody)
	require.NoError(t, err)
	assert.Equal(t, "success", respBody.Status)
	assert.Equal(t, "my-team-meeting", respBody.Room.Slug)
	assert.Equal(t, hostID.String(), respBody.Room.HostID)
	assert.NotEmpty(t, respBody.Room.ID)
}

func TestAPI_CreateRoomHandler_GuestForbidden(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)

	guestID, _ := uuid.NewV7()
	guestToken, _ := auth.GenerateTokenWithName(guestID.String(), "Budi", "guest", secret, 1*time.Hour)

	reqBody := `{"slug":"unauthorized-room"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+guestToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAPI_GetRoomHandler(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, 1*time.Hour)

	// First create the room
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"check-room"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	createW := httptest.NewRecorder()
	handler.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusOK, createW.Code)

	// Now get the room
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/check-room", nil)
	getW := httptest.NewRecorder()
	handler.ServeHTTP(getW, getReq)

	assert.Equal(t, http.StatusOK, getW.Code)
	var getResp struct {
		Slug             string `json:"slug"`
		ParticipantCount int    `json:"participant_count"`
	}
	err := json.NewDecoder(getW.Body).Decode(&getResp)
	require.NoError(t, err)
	assert.Equal(t, "check-room", getResp.Slug)
	assert.Equal(t, 0, getResp.ParticipantCount)
}

