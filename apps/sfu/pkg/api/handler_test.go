package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/naka-meet/sfu/pkg/api"
	"github.com/naka-meet/sfu/pkg/auth"
	"github.com/naka-meet/sfu/pkg/db"
	"github.com/naka-meet/sfu/pkg/room"
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

// fakeUserStore is an in-memory db.UserStore so signup/login can be unit
// tested without a real Postgres connection (TESTING_STRATEGY.md: mock
// external services at the unit level).
type fakeUserStore struct {
	mu    sync.Mutex
	users map[string]*db.User // keyed by lowercased email
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{users: make(map[string]*db.User)}
}

func (f *fakeUserStore) CreateUser(ctx context.Context, u *db.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.users[u.Email]; exists {
		return assert.AnError
	}
	f.users[u.Email] = u
	return nil
}

func (f *fakeUserStore) FindUserByEmail(ctx context.Context, email string) (*db.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[email]
	if !ok {
		return nil, db.ErrUserNotFound
	}
	return u, nil
}

func newHandlerWithUserStore(secret []byte) (*api.APIHandler, *fakeUserStore) {
	h := api.NewAPIHandler(secret, nil)
	store := newFakeUserStore()
	h.SetUserStoreForTesting(store)
	return h, store
}

func TestAPI_Signup_CreatesHostAccountAndSession(t *testing.T) {
	secret := []byte("api-secret-key")
	handler, _ := newHandlerWithUserStore(secret)

	reqBody := `{"name":"Alice","email":"alice@example.com","password":"correct-password-123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var jwtCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "jwt_token" {
			jwtCookie = c
		}
	}
	require.NotNil(t, jwtCookie)

	claims, err := auth.ValidateToken(jwtCookie.Value, secret)
	require.NoError(t, err)
	assert.Equal(t, "host", claims.Role)
	assert.Equal(t, "Alice", claims.Name)
}

func TestAPI_Signup_RejectsShortPassword(t *testing.T) {
	secret := []byte("api-secret-key")
	handler, _ := newHandlerWithUserStore(secret)

	reqBody := `{"name":"Alice","email":"alice@example.com","password":"short"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_Signup_RejectsDuplicateEmail(t *testing.T) {
	secret := []byte("api-secret-key")
	handler, store := newHandlerWithUserStore(secret)
	_ = store.CreateUser(context.Background(), &db.User{ID: "existing", Email: "alice@example.com", PasswordHash: "x"})

	reqBody := `{"name":"Alice","email":"alice@example.com","password":"correct-password-123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestAPI_Signup_WithoutDatabaseConfigured_Returns503(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil) // no user store set

	reqBody := `{"name":"Alice","email":"alice@example.com","password":"correct-password-123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestAPI_Login_SucceedsWithCorrectCredentials(t *testing.T) {
	secret := []byte("api-secret-key")
	handler, _ := newHandlerWithUserStore(secret)

	signupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", strings.NewReader(
		`{"name":"Alice","email":"alice@example.com","password":"correct-password-123"}`))
	handler.ServeHTTP(httptest.NewRecorder(), signupReq)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(
		`{"email":"alice@example.com","password":"correct-password-123"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, loginReq)

	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "host", body["role"])
	assert.Equal(t, "Alice", body["name"])
}

func TestAPI_Login_RejectsWrongPassword(t *testing.T) {
	secret := []byte("api-secret-key")
	handler, _ := newHandlerWithUserStore(secret)

	signupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", strings.NewReader(
		`{"name":"Alice","email":"alice@example.com","password":"correct-password-123"}`))
	handler.ServeHTTP(httptest.NewRecorder(), signupReq)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(
		`{"email":"alice@example.com","password":"wrong-password"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, loginReq)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPI_Login_RejectsUnknownEmail(t *testing.T) {
	secret := []byte("api-secret-key")
	handler, _ := newHandlerWithUserStore(secret)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(
		`{"email":"nobody@example.com","password":"whatever123"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, loginReq)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPI_GuestJoin_IgnoresClientSuppliedHostRole(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)

	// A guest attempting to self-declare role:"host" must still only ever
	// receive a "guest" JWT — this is the fix for the privilege-escalation
	// bug where /auth/login used to trust a client-supplied role verbatim.
	reqBody := `{"name":"Budi","room_slug":"demo-room","role":"host"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Guest joins hand the token back in the JSON body only — see
	// TestAPI_GuestJoin_DoesNotSetSessionCookie for why they must not also
	// set the jwt_token cookie.
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotEmpty(t, body["token"])

	claims, err := auth.ValidateToken(body["token"], secret)
	require.NoError(t, err)
	assert.Equal(t, "guest", claims.Role)
	assert.Equal(t, "Budi", claims.Name)
}

// TestAPI_GuestJoin_DoesNotSetSessionCookie is the regression test for the
// cross-tab session hijack bug: a guest/egress join used to call
// issueSession the same way host signup/login does, setting the site-wide
// jwt_token cookie. Because browsers share one cookie jar per origin across
// tabs, a guest joining in tab B silently overwrote the host's jwt_token
// cookie from tab A. extractAndValidateToken prefers the cookie over the
// Authorization header, so the host's own next REST call (e.g. "Record")
// got authenticated as the guest and 403'd — even though the host's React
// state still held a perfectly valid host token. Guests never rely on the
// cookie (guestJoin() in the frontend reads the token from the JSON body,
// and getSession()'s cookie-restore flow is gated to role === "host"), so
// the fix is to simply never set it for guest/egress sessions.
func TestAPI_GuestJoin_DoesNotSetSessionCookie(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)

	reqBody := `{"name":"Budi","room_slug":"demo-room","role":"guest"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	for _, c := range resp.Cookies() {
		assert.NotEqual(t, "jwt_token", c.Name, "guest join must not set/overwrite the jwt_token cookie shared with the host's tab")
	}
}

// TestAPI_GuestJoin_DoesNotClobberHostSessionCookie simulates the exact
// two-tab scenario: a host's tab_A response already carries a jwt_token
// cookie, then a guest joins from tab_B against the same handler instance.
// The guest response must not carry a Set-Cookie for jwt_token that would
// overwrite tab_A's cookie in a shared browser cookie jar.
func TestAPI_GuestJoin_DoesNotClobberHostSessionCookie(t *testing.T) {
	secret := []byte("api-secret-key")
	handler, _ := newHandlerWithUserStore(secret)

	signupReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", strings.NewReader(
		`{"name":"Alice","email":"alice@example.com","password":"correct-password-123"}`))
	signupW := httptest.NewRecorder()
	handler.ServeHTTP(signupW, signupReq)

	var hostCookie *http.Cookie
	for _, c := range signupW.Result().Cookies() {
		if c.Name == "jwt_token" {
			hostCookie = c
		}
	}
	require.NotNil(t, hostCookie, "host signup must set the jwt_token cookie")

	guestReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", strings.NewReader(
		`{"name":"Budi","room_slug":"demo-room","role":"guest"}`))
	guestW := httptest.NewRecorder()
	handler.ServeHTTP(guestW, guestReq)

	for _, c := range guestW.Result().Cookies() {
		assert.NotEqual(t, "jwt_token", c.Name, "guest join in a second tab must not overwrite the host's jwt_token cookie")
	}
}

func TestAPI_GuestJoin_AllowsEgressRole(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)

	reqBody := `{"name":"Egress Recorder","room_slug":"demo-room","role":"egress"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "egress", body["role"])
}

func TestAPI_Me_ReturnsSessionFromCookie(t *testing.T) {
	secret := []byte("api-secret-key")
	userID, _ := uuid.NewV7()
	token, _ := auth.GenerateTokenWithName(userID.String(), "Alice", "host", secret, time.Hour)

	handler := api.NewAPIHandler(secret, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: "jwt_token", Value: token})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "host", body["role"])
	assert.Equal(t, "Alice", body["name"])
}

func TestAPI_Me_UnauthenticatedWithoutCookie(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPI_Logout_ClearsCookie(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var jwtCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "jwt_token" {
			jwtCookie = c
		}
	}
	require.NotNil(t, jwtCookie)
	assert.True(t, jwtCookie.MaxAge < 0)
}

func TestAPI_EgressTriggerHandler_OwnerCanTrigger(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	reqBody := `{"action":"START_RECORDING"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/live", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "channel:egress_commands", mockPub.PublishedChannel)

	var payload map[string]string
	require.NoError(t, json.Unmarshal([]byte(mockPub.PublishedMessage), &payload))
	assert.Equal(t, "START_RECORDING", payload["action"])
	assert.Equal(t, "demo-room", payload["room"])
}

func TestAPI_EgressTriggerHandler_NonOwnerHostForbidden(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	// A different "host"-role JWT (e.g. a legitimate host of some other
	// room) must not be able to trigger recording on someone else's room.
	otherHostID, _ := uuid.NewV7()
	otherHostToken, _ := auth.GenerateTokenWithName(otherHostID.String(), "Other Host", "host", secret, time.Hour)

	reqBody := `{"action":"START_RECORDING"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/live", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+otherHostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, mockPub.PublishedChannel)
}

func TestAPI_EgressTriggerHandler_StopEgress(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	reqBody := `{"action":"STOP_EGRESS"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/live", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var payload map[string]string
	_ = json.Unmarshal([]byte(mockPub.PublishedMessage), &payload)
	assert.Equal(t, "STOP_EGRESS", payload["action"])
	assert.Equal(t, "demo-room", payload["room"])
}

func TestAPI_EgressTriggerHandler_StartRTMP(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	rtmpURL := "rtmp://a.rtmp.youtube.com/live2/key-xyz"
	reqBody := `{"action":"START_RTMP","url":"` + rtmpURL + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/live", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var payload map[string]string
	require.NoError(t, json.Unmarshal([]byte(mockPub.PublishedMessage), &payload))
	assert.Equal(t, "START_RTMP", payload["action"])
	assert.Equal(t, "demo-room", payload["room"])
	assert.Equal(t, rtmpURL, payload["url"])
}

func TestAPI_EgressTriggerHandler_BroadcastsRecordingConsentState(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	var gotSlug string
	var gotActive bool
	var gotKind string
	handler.SetRecordingBroadcaster(func(slug string, active bool, kind string) {
		gotSlug, gotActive, gotKind = slug, active, kind
	})

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/live", strings.NewReader(`{"action":"START_RECORDING"}`))
	req.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "demo-room", gotSlug)
	assert.True(t, gotActive)
	assert.Equal(t, "recording", gotKind)
}

func TestAPI_RemoveParticipantHandler_HostCanRemove(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	var gotSlug, gotParticipantID, gotReason string
	handler.SetParticipantRemover(func(slug, participantID, reason string) bool {
		gotSlug, gotParticipantID, gotReason = slug, participantID, reason
		return true
	})

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	guestID, _ := uuid.NewV7()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/participants/"+guestID.String()+"/remove", nil)
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "demo-room", gotSlug)
	assert.Equal(t, guestID.String(), gotParticipantID)
	assert.Equal(t, "host_removed", gotReason)
}

func TestAPI_RemoveParticipantHandler_NonOwnerHostForbidden(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	removerCalled := false
	handler.SetParticipantRemover(func(string, string, string) bool {
		removerCalled = true
		return true
	})

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	otherHostID, _ := uuid.NewV7()
	otherHostToken, _ := auth.GenerateTokenWithName(otherHostID.String(), "Other Host", "host", secret, time.Hour)

	guestID, _ := uuid.NewV7()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/participants/"+guestID.String()+"/remove", nil)
	req.Header.Set("Authorization", "Bearer "+otherHostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, removerCalled, "an unrelated host's JWT must never reach the actual removal callback")
}

func TestAPI_RemoveParticipantHandler_GuestForbidden(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	removerCalled := false
	handler.SetParticipantRemover(func(string, string, string) bool {
		removerCalled = true
		return true
	})

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	guestID, _ := uuid.NewV7()
	guestToken, _ := auth.GenerateTokenWithName(guestID.String(), "Guest", "guest", secret, time.Hour)
	otherID, _ := uuid.NewV7()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/participants/"+otherID.String()+"/remove", nil)
	req.Header.Set("Authorization", "Bearer "+guestToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, removerCalled)
}

func TestAPI_RemoveParticipantHandler_CannotTargetSelf(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	removerCalled := false
	handler.SetParticipantRemover(func(string, string, string) bool {
		removerCalled = true
		return true
	})

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/participants/"+hostID.String()+"/remove", nil)
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, removerCalled, "a host targeting their own ID should be rejected before reaching the removal callback")
}

func TestAPI_RemoveParticipantHandler_UnknownParticipantReturns404(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	// Simulates the participant already having disconnected on their own,
	// or an ID that was never valid to begin with.
	handler.SetParticipantRemover(func(string, string, string) bool { return false })

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	guestID, _ := uuid.NewV7()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/participants/"+guestID.String()+"/remove", nil)
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPI_MuteParticipantHandler_HostCanMute(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	var gotSlug, gotParticipantID string
	handler.SetParticipantMuter(func(slug, participantID string) bool {
		gotSlug, gotParticipantID = slug, participantID
		return true
	})

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	guestID, _ := uuid.NewV7()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/participants/"+guestID.String()+"/mute", nil)
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "demo-room", gotSlug)
	assert.Equal(t, guestID.String(), gotParticipantID)
}

func TestAPI_MuteParticipantHandler_NonOwnerHostForbidden(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	muterCalled := false
	handler.SetParticipantMuter(func(string, string) bool {
		muterCalled = true
		return true
	})

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	otherHostID, _ := uuid.NewV7()
	otherHostToken, _ := auth.GenerateTokenWithName(otherHostID.String(), "Other Host", "host", secret, time.Hour)

	guestID, _ := uuid.NewV7()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/participants/"+guestID.String()+"/mute", nil)
	req.Header.Set("Authorization", "Bearer "+otherHostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, muterCalled, "an unrelated host's JWT must never reach the actual mute callback")
}

func TestAPI_MuteParticipantHandler_GuestForbidden(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	muterCalled := false
	handler.SetParticipantMuter(func(string, string) bool {
		muterCalled = true
		return true
	})

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	guestID, _ := uuid.NewV7()
	guestToken, _ := auth.GenerateTokenWithName(guestID.String(), "Guest", "guest", secret, time.Hour)
	otherID, _ := uuid.NewV7()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/participants/"+otherID.String()+"/mute", nil)
	req.Header.Set("Authorization", "Bearer "+guestToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, muterCalled)
}

func TestAPI_MuteParticipantHandler_CannotTargetSelf(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	muterCalled := false
	handler.SetParticipantMuter(func(string, string) bool {
		muterCalled = true
		return true
	})

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/participants/"+hostID.String()+"/mute", nil)
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, muterCalled, "a host targeting their own ID should be rejected before reaching the mute callback")
}

func TestAPI_MuteParticipantHandler_UnknownParticipantReturns404(t *testing.T) {
	secret := []byte("api-secret-key")
	mockPub := &MockPublisher{}
	rm := room.NewRoomManager(nil)
	handler := api.NewAPIHandlerWithDeps(secret, mockPub, rm, nil)

	handler.SetParticipantMuter(func(string, string) bool { return false })

	hostID, _ := uuid.NewV7()
	hostToken, _ := auth.GenerateTokenWithName(hostID.String(), "Host User", "host", secret, time.Hour)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms", strings.NewReader(`{"slug":"demo-room"}`))
	createReq.Header.Set("Authorization", "Bearer "+hostToken)
	handler.ServeHTTP(httptest.NewRecorder(), createReq)

	guestID, _ := uuid.NewV7()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/demo-room/participants/"+guestID.String()+"/mute", nil)
	req.Header.Set("Authorization", "Bearer "+hostToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPI_IceServersHandler_WithoutTurnConfiguredReturnsStunOnly(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)
	// SetTurnConfig deliberately not called — TURN must stay opt-in, never
	// a hard requirement for a deployment that hasn't set up coturn.

	userID, _ := uuid.NewV7()
	token, _ := auth.GenerateTokenWithName(userID.String(), "Guest", "guest", secret, time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ice-servers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		IceServers []struct {
			URLs       []string `json:"urls"`
			Username   string   `json:"username"`
			Credential string   `json:"credential"`
		} `json:"iceServers"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	require.Len(t, body.IceServers, 1, "no TURN config means exactly the one default STUN entry")
	assert.Empty(t, body.IceServers[0].Username)
	assert.Empty(t, body.IceServers[0].Credential)
}

func TestAPI_IceServersHandler_WithTurnConfiguredIncludesTimeLimitedCredentials(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)
	handler.SetTurnConfig("turn-shared-secret", "turn.example.com", "3478")

	userID, _ := uuid.NewV7()
	token, _ := auth.GenerateTokenWithName(userID.String(), "Guest", "guest", secret, time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ice-servers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		IceServers []struct {
			URLs       []string `json:"urls"`
			Username   string   `json:"username"`
			Credential string   `json:"credential"`
		} `json:"iceServers"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	require.Len(t, body.IceServers, 3, "default STUN, coturn's own STUN, and the TURN entry")

	turnEntry := body.IceServers[2]
	assert.Contains(t, turnEntry.URLs[0], "turn:turn.example.com:3478")
	assert.NotEmpty(t, turnEntry.Username, "TURN entries need a username, unlike STUN")
	assert.NotEmpty(t, turnEntry.Credential)

	// The username must decode to the real requester's ID, not a shared or
	// blank placeholder — this is what would let coturn/an operator trace
	// relay usage back to a specific session if ever needed.
	assert.Contains(t, turnEntry.Username, userID.String())
}

func TestAPI_IceServersHandler_RejectsUnauthenticatedRequest(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)
	handler.SetTurnConfig("turn-shared-secret", "turn.example.com", "3478")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ice-servers", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
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

func TestAPI_CORS_RejectsUnknownOrigin(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)
	handler.SetAllowedOrigins([]string{"https://trusted.example.com"})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/rooms/demo-room", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestAPI_CORS_AllowsConfiguredOrigin(t *testing.T) {
	secret := []byte("api-secret-key")
	handler := api.NewAPIHandler(secret, nil)
	handler.SetAllowedOrigins([]string{"https://trusted.example.com"})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/rooms/demo-room", nil)
	req.Header.Set("Origin", "https://trusted.example.com")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, "https://trusted.example.com", w.Header().Get("Access-Control-Allow-Origin"))
}
