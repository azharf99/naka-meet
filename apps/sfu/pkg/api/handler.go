package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/naka-meet/sfu/pkg/auth"
	"github.com/naka-meet/sfu/pkg/db"
	"github.com/naka-meet/sfu/pkg/room"
	"gorm.io/gorm"
)

type RedisPublisher interface {
	Publish(ctx context.Context, channel string, message interface{}) error
}

type APIHandler struct {
	jwtSecret []byte
	publisher RedisPublisher
	rm        *room.RoomManager
	db        *gorm.DB
}

func NewAPIHandler(jwtSecret []byte, publisher RedisPublisher) *APIHandler {
	return &APIHandler{
		jwtSecret: jwtSecret,
		publisher: publisher,
	}
}

func NewAPIHandlerWithDeps(jwtSecret []byte, publisher RedisPublisher, rm *room.RoomManager, gormDB *gorm.DB) *APIHandler {
	return &APIHandler{
		jwtSecret: jwtSecret,
		publisher: publisher,
		rm:        rm,
		db:        gormDB,
	}
}

type LoginRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type EgressRequest struct {
	Action string `json:"action"`
	URL    string `json:"url,omitempty"`
}

func (h *APIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS Headers
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	path := r.URL.Path

	// 1. Auth Login Endpoint
	if path == "/api/v1/auth/login" && r.Method == http.MethodPost {
		var req LoginRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		role := req.Role
		if role == "" {
			role = "host"
		}
		name := req.Name
		if name == "" {
			name = "Anonymous"
		}

		userID, err := uuid.NewV7()
		if err != nil {
			http.Error(w, "Failed to generate user ID", http.StatusInternalServerError)
			return
		}

		tokenStr, err := auth.GenerateTokenWithName(userID.String(), name, role, h.jwtSecret, 24*time.Hour)
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
			Secure:   false,
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"token":   tokenStr,
			"user_id": userID.String(),
			"name":    name,
			"role":    role,
		})
		return
	}

	// 2. Create Room Endpoint (/api/v1/rooms)
	if path == "/api/v1/rooms" && r.Method == http.MethodPost {
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
		return
	}

	// 3. Room Egress Endpoint: /api/v1/rooms/:slug/live
	if strings.HasPrefix(path, "/api/v1/rooms/") && strings.HasSuffix(path, "/live") && r.Method == http.MethodPost {
		trimmed := strings.TrimPrefix(path, "/api/v1/rooms/")
		roomSlug := strings.TrimSuffix(trimmed, "/live")
		if roomSlug == "" {
			roomSlug = "demo-room"
		}

		claims := h.extractAndValidateToken(r)
		if claims == nil || claims.Role != "host" {
			http.Error(w, "Forbidden: Host authority required", http.StatusForbidden)
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "egress_triggered",
			"action": action,
			"room":   roomSlug,
		})
		return
	}

	// 4. Get Room Info Endpoint (/api/v1/rooms/:slug)
	if strings.HasPrefix(path, "/api/v1/rooms/") && r.Method == http.MethodGet {
		slug := strings.TrimPrefix(path, "/api/v1/rooms/")
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
		return
	}

	http.Error(w, "Not Found", http.StatusNotFound)
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

