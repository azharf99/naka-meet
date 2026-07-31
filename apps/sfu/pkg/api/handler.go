package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/naka-meet/sfu/pkg/auth"
	"github.com/naka-meet/sfu/pkg/db"
	"github.com/naka-meet/sfu/pkg/room"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

type RedisPublisher interface {
	Publish(ctx context.Context, channel string, message interface{}) error
}

// dummyPasswordHash is compared against on a login attempt for an email that
// doesn't exist, so that "unknown email" and "wrong password" take roughly
// the same amount of time to answer — otherwise the response latency itself
// would let an attacker distinguish which case failed (user enumeration).
var dummyPasswordHash, _ = auth.HashPassword("nakameet-dummy-timing-safety-password")

type APIHandler struct {
	jwtSecret      []byte
	publisher      RedisPublisher
	rm             *room.RoomManager
	db             *gorm.DB
	userStore      db.UserStore
	allowedOrigins []string
	secureCookies  bool

	// recordingBroadcast, when set, notifies every participant in a room
	// (not just the host whose click triggered it) that a recording/RTMP
	// stream started or stopped, for on-screen consent notice.
	recordingBroadcast func(roomSlug string, active bool, kind string)

	authLimiters  map[string]*rate.Limiter
	authLimiterMu sync.Mutex
}

func NewAPIHandler(jwtSecret []byte, publisher RedisPublisher) *APIHandler {
	return &APIHandler{
		jwtSecret: jwtSecret,
		publisher: publisher,
	}
}

func NewAPIHandlerWithDeps(jwtSecret []byte, publisher RedisPublisher, rm *room.RoomManager, gormDB *gorm.DB) *APIHandler {
	h := &APIHandler{
		jwtSecret: jwtSecret,
		publisher: publisher,
		rm:        rm,
		db:        gormDB,
	}
	if gormDB != nil {
		h.userStore = db.NewGormUserStore(gormDB)
	}
	return h
}

// SetAllowedOrigins configures the CORS/cookie-sending allowlist (typically
// derived from the FRONTEND_URL env var). Cross-origin browser requests from
// any other Origin never get an Access-Control-Allow-Origin header, so the
// browser blocks the response instead of exposing it to an attacker's page.
func (h *APIHandler) SetAllowedOrigins(origins []string) {
	h.allowedOrigins = origins
}

// SetSecureCookies controls the jwt_token cookie's Secure flag. Should be
// true whenever the deployment is served over HTTPS.
func (h *APIHandler) SetSecureCookies(secure bool) {
	h.secureCookies = secure
}

// SetRecordingBroadcaster wires a callback (typically signaling.Handler's
// room broadcast) so every participant — not just the host who clicked the
// button — learns a recording/RTMP stream started or stopped.
func (h *APIHandler) SetRecordingBroadcaster(fn func(roomSlug string, active bool, kind string)) {
	h.recordingBroadcast = fn
}

// SetUserStoreForTesting overrides the user store (e.g. with an in-memory
// fake) without requiring a real Postgres connection. Test-only.
func (h *APIHandler) SetUserStoreForTesting(store db.UserStore) {
	h.userStore = store
}

func (h *APIHandler) isAllowedOrigin(origin string) bool {
	for _, o := range h.allowedOrigins {
		if o == origin {
			return true
		}
	}
	return false
}

func (h *APIHandler) applyCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" || !h.isAllowedOrigin(origin) {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// allowAuthAttempt rate-limits password-guessing traffic per source IP. A
// single-process in-memory limiter matches the app's current single-VPS
// architecture (NFR3 already flags Redis-backed distributed state as a
// future migration, not a current requirement).
func (h *APIHandler) allowAuthAttempt(r *http.Request) bool {
	ip := clientIP(r)
	h.authLimiterMu.Lock()
	defer h.authLimiterMu.Unlock()
	if h.authLimiters == nil {
		h.authLimiters = make(map[string]*rate.Limiter)
	}
	lim, ok := h.authLimiters[ip]
	if !ok {
		lim = rate.NewLimiter(rate.Every(2*time.Second), 10)
		h.authLimiters[ip] = lim
	}
	return lim.Allow()
}

type SignupRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// GuestRequest is the ephemeral, no-password join used by guests and by the
// egress recorder bot. Role is server-validated below — a client can never
// obtain a "host" JWT through this path.
type GuestRequest struct {
	Name     string `json:"name"`
	RoomSlug string `json:"room_slug"`
	Role     string `json:"role"`
}

type EgressRequest struct {
	Action string `json:"action"`
	URL    string `json:"url,omitempty"`
}

func (h *APIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.applyCORS(w, r)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	path := r.URL.Path

	// 1. Real host account creation (email + password, persisted to Postgres)
	if path == "/api/v1/auth/signup" && r.Method == http.MethodPost {
		h.handleSignup(w, r)
		return
	}

	// 2. Real host login (email + password against Postgres)
	if path == "/api/v1/auth/login" && r.Method == http.MethodPost {
		h.handleLogin(w, r)
		return
	}

	// 3. Ephemeral guest/egress join — no password, no DB row, role locked
	// server-side to "guest"/"egress" (never "host").
	if path == "/api/v1/auth/guest" && r.Method == http.MethodPost {
		h.handleGuestJoin(w, r)
		return
	}

	// 4. Session restore from the httpOnly cookie (skip re-entering credentials
	// after a page reload).
	if path == "/api/v1/auth/me" && r.Method == http.MethodGet {
		h.handleMe(w, r)
		return
	}

	// 5. Logout
	if path == "/api/v1/auth/logout" && r.Method == http.MethodPost {
		h.handleLogout(w, r)
		return
	}

	// 6. Create Room Endpoint (/api/v1/rooms)
	if path == "/api/v1/rooms" && r.Method == http.MethodPost {
		h.handleCreateRoom(w, r)
		return
	}

	// 7. Room Egress Endpoint: /api/v1/rooms/:slug/live
	if strings.HasPrefix(path, "/api/v1/rooms/") && strings.HasSuffix(path, "/live") && r.Method == http.MethodPost {
		h.handleEgressTrigger(w, r)
		return
	}

	// 8. Get Room Info Endpoint (/api/v1/rooms/:slug)
	if strings.HasPrefix(path, "/api/v1/rooms/") && r.Method == http.MethodGet {
		h.handleGetRoom(w, r)
		return
	}

	http.Error(w, "Not Found", http.StatusNotFound)
}

func (h *APIHandler) handleSignup(w http.ResponseWriter, r *http.Request) {
	if !h.allowAuthAttempt(r) {
		http.Error(w, "Too many attempts, please try again later", http.StatusTooManyRequests)
		return
	}
	if h.userStore == nil {
		http.Error(w, "Signup requires a configured database", http.StatusServiceUnavailable)
		return
	}

	var req SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(req.Name)
	email := strings.ToLower(strings.TrimSpace(req.Email))

	if name == "" || email == "" || len(req.Password) < 8 {
		http.Error(w, "Name, a valid email, and a password of at least 8 characters are required", http.StatusBadRequest)
		return
	}
	if _, err := mail.ParseAddress(email); err != nil {
		http.Error(w, "Invalid email address", http.StatusBadRequest)
		return
	}

	if _, err := h.userStore.FindUserByEmail(r.Context(), email); err == nil {
		http.Error(w, "An account with this email already exists", http.StatusConflict)
		return
	} else if !errors.Is(err, db.ErrUserNotFound) {
		http.Error(w, "Failed to check existing account", http.StatusInternalServerError)
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		http.Error(w, "Failed to create account", http.StatusInternalServerError)
		return
	}

	userID, err := uuid.NewV7()
	if err != nil {
		http.Error(w, "Failed to generate user ID", http.StatusInternalServerError)
		return
	}

	newUser := &db.User{
		ID:           userID.String(),
		Name:         name,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	}
	if err := h.userStore.CreateUser(r.Context(), newUser); err != nil {
		http.Error(w, "An account with this email already exists", http.StatusConflict)
		return
	}

	h.issueSession(w, userID.String(), name, "host")
}

func (h *APIHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !h.allowAuthAttempt(r) {
		http.Error(w, "Too many attempts, please try again later", http.StatusTooManyRequests)
		return
	}
	if h.userStore == nil {
		http.Error(w, "Login requires a configured database", http.StatusServiceUnavailable)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))

	user, err := h.userStore.FindUserByEmail(r.Context(), email)
	if err != nil {
		_ = auth.ComparePassword(dummyPasswordHash, req.Password)
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}
	if err := auth.ComparePassword(user.PasswordHash, req.Password); err != nil {
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	h.issueSession(w, user.ID, user.Name, "host")
}

func (h *APIHandler) handleGuestJoin(w http.ResponseWriter, r *http.Request) {
	var req GuestRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Guest"
	}

	// The client-supplied role is never trusted beyond this allowlist — a
	// "host" JWT can only ever come from handleSignup/handleLogin.
	role := "guest"
	if req.Role == "egress" {
		role = "egress"
	}

	userID, err := uuid.NewV7()
	if err != nil {
		http.Error(w, "Failed to generate user ID", http.StatusInternalServerError)
		return
	}

	h.issueSession(w, userID.String(), name, role)
}

func (h *APIHandler) handleMe(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("jwt_token")
	if err != nil || cookie.Value == "" {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}
	claims, err := auth.ValidateToken(cookie.Value, h.jwtSecret)
	if err != nil {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"user_id": claims.UserID,
		"name":    claims.Name,
		"role":    claims.Role,
	})
}

func (h *APIHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "jwt_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.secureCookies,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusOK)
}

func (h *APIHandler) issueSession(w http.ResponseWriter, userID, name, role string) {
	tokenStr, err := auth.GenerateTokenWithName(userID, name, role, h.jwtSecret, 24*time.Hour)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "jwt_token",
		Value:    tokenStr,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.secureCookies,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"token":   tokenStr,
		"user_id": userID,
		"name":    name,
		"role":    role,
	})
}

func (h *APIHandler) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	claims := h.extractAndValidateToken(r)
	if claims == nil || claims.Role != "host" {
		http.Error(w, "Forbidden: Host authority required", http.StatusForbidden)
		return
	}

	var req struct {
		Slug string `json:"slug"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slugUUID, _ := uuid.NewV7()
		slug = "room-" + slugUUID.String()[:8]
	}

	roomID, _ := uuid.NewV7()

	if h.db != nil {
		newRoom := db.Room{
			ID:        roomID.String(),
			Slug:      slug,
			HostID:    claims.UserID,
			CreatedAt: time.Now(),
		}
		_ = h.db.Create(&newRoom)
	}

	if h.rm != nil {
		_, _ = h.rm.CreateOrGetRoom(r.Context(), slug, claims.UserID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"room": map[string]string{
			"id":      roomID.String(),
			"slug":    slug,
			"host_id": claims.UserID,
		},
	})
}

func (h *APIHandler) handleEgressTrigger(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/rooms/")
	roomSlug := strings.TrimSuffix(trimmed, "/live")
	if roomSlug == "" {
		roomSlug = "demo-room"
	}

	claims := h.extractAndValidateToken(r)
	if !h.isRoomHost(claims, roomSlug) {
		http.Error(w, "Forbidden: Host authority required for this room", http.StatusForbidden)
		return
	}

	var req EgressRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	action := req.Action
	if action == "" {
		action = "START_RECORDING"
	}

	payload := map[string]string{
		"action": action,
		"room":   roomSlug,
		"url":    req.URL,
	}

	payloadBytes, _ := json.Marshal(payload)

	if h.publisher != nil {
		_ = h.publisher.Publish(r.Context(), "channel:egress_commands", string(payloadBytes))
	}

	if h.recordingBroadcast != nil {
		switch action {
		case "START_RECORDING":
			h.recordingBroadcast(roomSlug, true, "recording")
		case "START_RTMP":
			h.recordingBroadcast(roomSlug, true, "rtmp")
		case "STOP_EGRESS":
			h.recordingBroadcast(roomSlug, false, "")
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "egress_triggered",
		"action": action,
		"room":   roomSlug,
	})
}

func (h *APIHandler) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/rooms/")
	slug = strings.TrimSuffix(slug, "/")
	if slug == "" {
		http.Error(w, "Bad Request: missing room slug", http.StatusBadRequest)
		return
	}

	count := 0
	if h.rm != nil {
		count = h.rm.GetParticipantCount(slug)
	} else if h.db != nil {
		var rModel db.Room
		if err := h.db.Where("slug = ?", slug).First(&rModel).Error; err != nil {
			http.Error(w, "Room not found", http.StatusNotFound)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"slug":              slug,
		"participant_count": count,
	})
}

// isRoomHost checks the claim's role AND that the caller is the room's
// actual owner (its real HostID), not merely someone holding a JWT that
// self-declares role:"host" — the room's HostID is set once, at creation
// time, by whoever legitimately created it via handleCreateRoom.
func (h *APIHandler) isRoomHost(claims *auth.Claims, roomSlug string) bool {
	if claims == nil || claims.Role != "host" {
		return false
	}

	if h.db != nil {
		var rModel db.Room
		if err := h.db.Where("slug = ?", roomSlug).First(&rModel).Error; err == nil {
			return rModel.HostID == claims.UserID
		}
		// Room not found in Postgres (e.g. DB configured but this room was
		// only ever created through the in-memory RoomManager) — fall
		// through to the in-memory check below rather than failing closed.
	}

	if h.rm != nil {
		if hostID, ok := h.rm.GetRoomHostID(roomSlug); ok {
			return hostID == claims.UserID
		}
	}

	return false
}

func (h *APIHandler) extractAndValidateToken(r *http.Request) *auth.Claims {
	var tokenStr string
	if cookie, err := r.Cookie("jwt_token"); err == nil && cookie.Value != "" {
		tokenStr = cookie.Value
	} else if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
	}
	if tokenStr == "" {
		return nil
	}
	claims, err := auth.ValidateToken(tokenStr, h.jwtSecret)
	if err != nil {
		return nil
	}
	return claims
}
