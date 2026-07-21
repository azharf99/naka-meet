package signaling

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/naka-meet/sfu/pkg/auth"
	"github.com/naka-meet/sfu/pkg/room"
	"github.com/naka-meet/sfu/pkg/webrtc"
)


type SignalMessage struct {
	Type      string          `json:"type"`
	SDP       string          `json:"sdp,omitempty"`
	Candidate json.RawMessage `json:"candidate,omitempty"`
	StreamID  string          `json:"stream_id,omitempty"`
	Kind      string          `json:"kind,omitempty"`
}

type Handler struct {
	rm        *room.RoomManager
	router    *webrtc.SFURouter
	jwtSecret []byte
	upgrader  websocket.Upgrader
	conns     map[string]map[string]*websocket.Conn
	mu        sync.RWMutex
}

func NewHandler(rm *room.RoomManager, router *webrtc.SFURouter, jwtSecret []byte) *Handler {
	return &Handler{
		rm:        rm,
		router:    router,
		jwtSecret: jwtSecret,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		conns: make(map[string]map[string]*websocket.Conn),
	}
}

func (h *Handler) registerConn(roomSlug, userID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.conns[roomSlug]; !exists {
		h.conns[roomSlug] = make(map[string]*websocket.Conn)
	}
	h.conns[roomSlug][userID] = conn
}

func (h *Handler) unregisterConn(roomSlug, userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if roomConns, exists := h.conns[roomSlug]; exists {
		delete(roomConns, userID)
		if len(roomConns) == 0 {
			delete(h.conns, roomSlug)
		}
	}
}

func (h *Handler) broadcastToRoom(roomSlug, senderID string, message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if roomConns, exists := h.conns[roomSlug]; exists {
		for uid, c := range roomConns {
			if uid != senderID {
				_ = c.WriteMessage(websocket.TextMessage, message)
			}
		}
	}
}


func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for cross-origin frontend (Port 3000 -> 8080)
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 1. Extract Token from Cookie, Query Param, or Authorization Header
	var tokenStr string
	if cookie, err := r.Cookie("jwt_token"); err == nil && cookie.Value != "" {
		tokenStr = cookie.Value
	} else if qToken := r.URL.Query().Get("token"); qToken != "" {
		tokenStr = qToken
	} else if qJwt := r.URL.Query().Get("jwt_token"); qJwt != "" {
		tokenStr = qJwt
	} else if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
	}

	if tokenStr == "" {
		http.Error(w, "Unauthorized: missing token", http.StatusUnauthorized)
		return
	}

	claims, err := auth.ValidateToken(tokenStr, h.jwtSecret)
	if err != nil {
		http.Error(w, "Unauthorized: invalid jwt token", http.StatusUnauthorized)
		return
	}

	roomSlug := r.URL.Query().Get("room_slug")
	if roomSlug == "" {
		roomSlug = "demo-room"
	}

	// 2. Upgrade to WebSocket
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade WebSocket: %v", err)
		return
	}
	defer conn.Close()

	// 3. Add to Room Manager
	displayName := claims.Name
	if displayName == "" {
		displayName = claims.UserID
	}
	p := &room.Participant{
		ID:       claims.UserID,
		Name:     displayName,
		JoinedAt: time.Now(),
	}


	_, _ = h.rm.CreateOrGetRoom(r.Context(), roomSlug, claims.UserID)
	if err := h.rm.AddParticipant(r.Context(), roomSlug, p); err != nil {
		log.Printf("Failed to add participant to room: %v", err)
		return
	}
	defer h.rm.HandleDisconnect(roomSlug, claims.UserID, 15*time.Second)

	// 4. Create PeerConnection in Pion Router
	_, err = h.router.AddPeer(claims.UserID)
	if err != nil {
		log.Printf("Failed to create PeerConnection: %v", err)
		return
	}

	h.registerConn(roomSlug, claims.UserID, conn)
	defer h.unregisterConn(roomSlug, claims.UserID)

	// 5. Message Loop
	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var msg SignalMessage
		if err := json.Unmarshal(messageBytes, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "offer":
			// Offer processing
		case "answer":
			// Answer processing
		case "candidate":
			// Candidate processing
		case "track_metadata":
			// BR4 Out-of-band labeling: broadcast metadata to all participants in room
			h.broadcastToRoom(roomSlug, claims.UserID, messageBytes)
		}
	}
}

