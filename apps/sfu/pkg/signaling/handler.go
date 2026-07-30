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
	Text      string          `json:"text,omitempty"`
	MediaKind string          `json:"media_kind,omitempty"`
	Enabled   bool            `json:"enabled,omitempty"`
}

// SafeConn wraps a *websocket.Conn with a mutex (gorilla/websocket panics on
// concurrent writes) and a write deadline, applied on every write, so a
// single stalled client can never block broadcastToRoom's write loop
// (held under Handler.mu) for the rest of the room indefinitely.
type SafeConn struct {
	conn      *websocket.Conn
	writeWait time.Duration
	mu        sync.Mutex
}

func (sc *SafeConn) WriteMessage(messageType int, data []byte) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	_ = sc.conn.SetWriteDeadline(time.Now().Add(sc.writeWait))
	return sc.conn.WriteMessage(messageType, data)
}

func (sc *SafeConn) Close() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.conn.Close()
}

type Handler struct {
	rm          *room.RoomManager
	router      *webrtc.SFURouter
	jwtSecret   []byte
	upgrader    websocket.Upgrader
	conns       map[string]map[string]*SafeConn
	connsByUser map[string]*SafeConn
	mu          sync.RWMutex

	// WebSocket liveness-detection intervals, per-Handler (not package
	// globals) so tests can shrink them on one Handler instance without
	// racing against other tests' connections/goroutines. Without these, a
	// stalled/dead connection is never proactively detected: a single slow
	// client's blocking write inside broadcastToRoom could stall delivery to
	// an entire room, and a dropped connection with no clean close would
	// never be reaped.
	wsWriteWait  time.Duration
	wsPongWait   time.Duration
	wsPingPeriod time.Duration
}

func NewHandler(rm *room.RoomManager, router *webrtc.SFURouter, jwtSecret []byte) *Handler {
	pongWait := 60 * time.Second
	h := &Handler{
		rm:        rm,
		router:    router,
		jwtSecret: jwtSecret,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		conns:        make(map[string]map[string]*SafeConn),
		connsByUser:  make(map[string]*SafeConn),
		wsWriteWait:  10 * time.Second,
		wsPongWait:   pongWait,
		wsPingPeriod: (pongWait * 9) / 10,
	}
	// Lets the router deliver a renegotiation offer to any peer (e.g. one
	// that was deferred because the peer's signaling state wasn't stable
	// when a track was published) without needing to know its room.
	router.SetOfferSender(h.sendOfferToPeer)
	return h
}

// SetKeepaliveIntervalsForTesting overrides this Handler's WS keepalive
// intervals. Test-only; call before any connections are made on this
// Handler, since existing connections capture the values at connect time.
func (h *Handler) SetKeepaliveIntervalsForTesting(ping, pong time.Duration) {
	h.wsWriteWait = ping
	h.wsPongWait = pong
	h.wsPingPeriod = ping
}

func (h *Handler) registerConn(roomSlug, userID string, conn *SafeConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.conns[roomSlug]; !exists {
		h.conns[roomSlug] = make(map[string]*SafeConn)
	}
	h.conns[roomSlug][userID] = conn
	h.connsByUser[userID] = conn
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
	delete(h.connsByUser, userID)
}

func (h *Handler) sendOfferToPeer(peerID, sdp string) {
	h.mu.RLock()
	conn, exists := h.connsByUser[peerID]
	h.mu.RUnlock()
	if !exists {
		return
	}
	msgBytes, _ := json.Marshal(map[string]interface{}{
		"type": "offer",
		"sdp":  sdp,
	})
	_ = conn.WriteMessage(websocket.TextMessage, msgBytes)
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
	h.router.BroadcastTrackAndRenegotiateWithMetadata(roomSlug, publisherID, publisherName, kind, track, h.sendOfferToPeer)
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
	safeConn := &SafeConn{conn: conn, writeWait: h.wsWriteWait}
	defer safeConn.Close()

	// Proactive liveness detection: without a deadline+pong refresh, a dead
	// TCP connection (no clean WS close) would sit registered forever, and
	// broadcastToRoom would keep trying to write to it. The read deadline is
	// refreshed on every pong; if pings stop getting answered (network died,
	// or this goroutine's peer is starved of CPU for too long), ReadMessage
	// in the main loop below eventually times out and triggers the normal
	// cleanup path — same effect as any other read error.
	_ = conn.SetReadDeadline(time.Now().Add(h.wsPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(h.wsPongWait))
	})

	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		ticker := time.NewTicker(h.wsPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := safeConn.WriteMessage(websocket.PingMessage, nil); err != nil {
					_ = safeConn.Close()
					return
				}
			case <-pingDone:
				return
			}
		}
	}()

	// 3. Add to Room Manager
	displayName := claims.Name
	if displayName == "" {
		displayName = claims.UserID
	}
	p := &room.Participant{
		ID:       claims.UserID,
		Name:     displayName,
		JoinedAt: time.Now(),
		IsEgress: claims.Role == "egress",
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
	// SetPeerRoom is deliberately NOT called here. Doing so would make this
	// peer a valid BroadcastTrackAndRenegotiateWithMetadata target before it
	// has processed its own initial "offer" below — a concurrent broadcast
	// could push a server-initiated offer into this still-stable PC, flip it
	// to HaveLocalOffer, and then the client's real offer arriving via the
	// message loop would be an invalid SetRemoteDescription transition and
	// get silently rejected, stranding this peer forever. SetPeerRoom is
	// called instead once this peer's own offer/answer exchange is durably
	// complete (see the "offer" case below).

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
				remoteErr := pc.SetRemoteDescription(offer)
				if remoteErr != nil && pc.SignalingState() == pion.SignalingStateHaveLocalOffer {
					// A server-initiated offer raced this client's own join
					// offer (see the AddPeer comment above) and won, leaving
					// the PC in HaveLocalOffer. Roll back once and retry —
					// this peer's own offer takes priority for its join.
					if rbErr := pc.SetLocalDescription(pion.SessionDescription{Type: pion.SDPTypeRollback}); rbErr != nil {
						log.Printf("offer: rollback failed for %s: %v", claims.UserID, rbErr)
					} else {
						remoteErr = pc.SetRemoteDescription(offer)
					}
				}
				if remoteErr != nil {
					log.Printf("offer: SetRemoteDescription failed for %s: %v", claims.UserID, remoteErr)
					continue
				}

				answer, err := pc.CreateAnswer(nil)
				if err != nil {
					log.Printf("offer: CreateAnswer failed for %s: %v", claims.UserID, err)
					continue
				}
				if err := pc.SetLocalDescription(answer); err != nil {
					log.Printf("offer: SetLocalDescription failed for %s: %v", claims.UserID, err)
					continue
				}

				ansBytes, _ := json.Marshal(map[string]interface{}{
					"type": "answer",
					"sdp":  answer.SDP,
				})
				_ = safeConn.WriteMessage(websocket.TextMessage, ansBytes)

				// Only now — after this peer's own offer/answer exchange is
				// durably complete — does it become a valid broadcast target
				// for other publishers' tracks. See the AddPeer comment above.
				h.router.SetPeerRoom(claims.UserID, roomSlug)

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

				// Routed through the router's serialized renegotiation path
				// (not an inline CreateOffer here) so this doesn't race a
				// concurrent renegotiation triggered by another publisher's
				// OnTrack callback firing for this same brand-new peer.
				if count > 0 {
					h.router.RenegotiatePeer(claims.UserID)
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

		case "media_state":
			// Self-mute/camera-off relay: a sender only ever toggles its own
			// local track's `.enabled`, which produces no observable event on
			// the corresponding remote track on other peers' PeerConnections
			// (per WebRTC spec) — so without this out-of-band message, other
			// participants have no way to know. peer_id/peer_name are always
			// server-enriched, never trusted from the client, matching
			// track_metadata's pattern above.
			if msg.MediaKind != "mic" && msg.MediaKind != "cam" {
				continue
			}
			stateBytes, _ := json.Marshal(map[string]interface{}{
				"type":       "media_state",
				"media_kind": msg.MediaKind,
				"enabled":    msg.Enabled,
				"peer_id":    claims.UserID,
				"peer_name":  displayName,
			})
			h.broadcastToRoom(roomSlug, claims.UserID, stateBytes)

		case "chat":
			// Every client only has a PeerConnection to the SFU (hub-and-spoke,
			// never peer-to-peer), so a browser-side RTCDataChannel message has
			// no path to other participants unless the server relays it. The
			// signaling WebSocket's existing room broadcast is that relay.
			if strings.TrimSpace(msg.Text) == "" {
				continue
			}
			chatBytes, _ := json.Marshal(map[string]interface{}{
				"type":    "chat",
				"text":    msg.Text,
				"sender":  displayName,
				"peer_id": claims.UserID,
				"time":    time.Now().Format("15:04"),
			})
			h.broadcastToRoom(roomSlug, claims.UserID, chatBytes)
		}
	}
}

