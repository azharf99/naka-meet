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
	pion "github.com/pion/webrtc/v4"
)

type SignalMessage struct {
	Type      string          `json:"type"`
	SDP       string          `json:"sdp,omitempty"`
	Candidate json.RawMessage `json:"candidate,omitempty"`
	StreamID  string          `json:"stream_id,omitempty"`
	Kind      string          `json:"kind,omitempty"`
}

type SafeConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (sc *SafeConn) WriteMessage(messageType int, data []byte) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.conn.WriteMessage(messageType, data)
}

func (sc *SafeConn) Close() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.conn.Close()
}

type Handler struct {
	rm        *room.RoomManager
	router    *webrtc.SFURouter
	jwtSecret []byte
	upgrader  websocket.Upgrader
	conns     map[string]map[string]*SafeConn
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
		conns: make(map[string]map[string]*SafeConn),
	}
}

func (h *Handler) registerConn(roomSlug, userID string, conn *SafeConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.conns[roomSlug]; !exists {
		h.conns[roomSlug] = make(map[string]*SafeConn)
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

func (h *Handler) AddTrackAndRenegotiate(roomSlug, publisherID string, track pion.TrackLocal) {
	h.AddTrackAndRenegotiateWithMetadata(roomSlug, publisherID, "", "camera", track)
}

func (h *Handler) AddTrackAndRenegotiateWithMetadata(roomSlug, publisherID, publisherName, kind string, track pion.TrackLocal) {
	h.router.BroadcastTrackAndRenegotiateWithMetadata(roomSlug, publisherID, publisherName, kind, track, func(targetUserID, offerSDP string) {
		h.mu.RLock()
		defer h.mu.RUnlock()
		if roomConns, exists := h.conns[roomSlug]; exists {
			if c, found := roomConns[targetUserID]; found {
				msgBytes, _ := json.Marshal(map[string]interface{}{
					"type": "offer",
					"sdp":  offerSDP,
				})
				_ = c.WriteMessage(websocket.TextMessage, msgBytes)
			}
		}
	})
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
	safeConn := &SafeConn{conn: conn}
	defer safeConn.Close()

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
	pc, err := h.router.AddPeer(claims.UserID)
	if err != nil {
		log.Printf("Failed to create PeerConnection: %v", err)
		return
	}
	h.router.SetPeerRoom(claims.UserID, roomSlug)

	h.registerConn(roomSlug, claims.UserID, safeConn)
	defer func() {
		h.unregisterConn(roomSlug, claims.UserID)
		_ = h.router.RemovePeer(claims.UserID)
		leftMsg, _ := json.Marshal(map[string]interface{}{
			"type":    "participant_left",
			"peer_id": claims.UserID,
		})
		h.broadcastToRoom(roomSlug, claims.UserID, leftMsg)
	}()

	// Attach ICE Candidate Listener on backend PeerConnection
	pc.OnICECandidate(func(c *pion.ICECandidate) {
		if c == nil {
			return
		}
		candJSON := c.ToJSON()
		msgBytes, _ := json.Marshal(map[string]interface{}{
			"type":      "candidate",
			"candidate": candJSON,
		})
		_ = safeConn.WriteMessage(websocket.TextMessage, msgBytes)
	})

	// Attach OnTrack Listener on backend PeerConnection for RTP track forwarding
	pc.OnTrack(func(remoteTrack *pion.TrackRemote, receiver *pion.RTPReceiver) {
		localTrack, err := pion.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, remoteTrack.ID(), remoteTrack.StreamID())
		if err != nil {
			log.Printf("Failed to create local track: %v", err)
			return
		}

		kind := "camera"
		if strings.Contains(strings.ToLower(remoteTrack.StreamID()), "screen") || strings.Contains(strings.ToLower(remoteTrack.ID()), "screen") {
			kind = "screen"
		}

		// Forward RTP Packets in Goroutine
		go func(remote *pion.TrackRemote, local *pion.TrackLocalStaticRTP) {
			buf := make([]byte, 1500)
			for {
				i, _, readErr := remote.Read(buf)
				if readErr != nil {
					break
				}
				if _, writeErr := local.Write(buf[:i]); writeErr != nil {
					break
				}
			}
		}(remoteTrack, localTrack)

		// Broadcast track metadata to all existing participants right when track is published
		metaBytes, _ := json.Marshal(map[string]interface{}{
			"type":      "track_metadata",
			"stream_id": localTrack.StreamID(),
			"track_id":  localTrack.ID(),
			"peer_id":   claims.UserID,
			"peer_name": displayName,
			"kind":      kind,
		})
		h.broadcastToRoom(roomSlug, claims.UserID, metaBytes)

		// Broadcast track to all other room participants & trigger SDP renegotiation
		h.AddTrackAndRenegotiateWithMetadata(roomSlug, claims.UserID, displayName, kind, localTrack)
	})

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
			if pc != nil && msg.SDP != "" {
				offer := pion.SessionDescription{
					Type: pion.SDPTypeOffer,
					SDP:  msg.SDP,
				}
				if err := pc.SetRemoteDescription(offer); err == nil {
					answer, err := pc.CreateAnswer(nil)
					if err == nil {
						if err := pc.SetLocalDescription(answer); err == nil {
							ansBytes, _ := json.Marshal(map[string]interface{}{
								"type": "answer",
								"sdp":  answer.SDP,
							})
							_ = safeConn.WriteMessage(websocket.TextMessage, ansBytes)

							// Subscribe peer to any pre-existing active room tracks AFTER initial answer is sent
							count, _ := h.router.SubscribePeerToRoomTracks(roomSlug, claims.UserID, func(rt *webrtc.RoomTrack) {
								metaBytes, _ := json.Marshal(map[string]interface{}{
									"type":      "track_metadata",
									"stream_id": rt.Track.StreamID(),
									"track_id":  rt.Track.ID(),
									"peer_id":   rt.PublisherID,
									"peer_name": rt.PublisherName,
									"kind":      rt.Kind,
								})
								_ = safeConn.WriteMessage(websocket.TextMessage, metaBytes)
							})

							if count > 0 && pc.SignalingState() == pion.SignalingStateStable {
								renegOffer, err := pc.CreateOffer(nil)
								if err == nil {
									if err := pc.SetLocalDescription(renegOffer); err == nil {
										offerBytes, _ := json.Marshal(map[string]interface{}{
											"type": "offer",
											"sdp":  renegOffer.SDP,
										})
										_ = safeConn.WriteMessage(websocket.TextMessage, offerBytes)
									}
								}
							}
						}
					}
				}
			}
		case "answer":
			if pc != nil && msg.SDP != "" {
				answer := pion.SessionDescription{
					Type: pion.SDPTypeAnswer,
					SDP:  msg.SDP,
				}
				_ = pc.SetRemoteDescription(answer)
			}
		case "candidate":
			if pc != nil && len(msg.Candidate) > 0 {
				var candInit pion.ICECandidateInit
				if err := json.Unmarshal(msg.Candidate, &candInit); err == nil {
					_ = pc.AddICECandidate(candInit)
				}
			}

		case "track_metadata":
			var rawMap map[string]interface{}
			if err := json.Unmarshal(messageBytes, &rawMap); err == nil {
				rawMap["peer_id"] = claims.UserID
				rawMap["peer_name"] = displayName
				enriched, _ := json.Marshal(rawMap)
				h.broadcastToRoom(roomSlug, claims.UserID, enriched)
			} else {
				h.broadcastToRoom(roomSlug, claims.UserID, messageBytes)
			}
		}
	}
}

