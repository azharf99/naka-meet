package room

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrRoomFull            = errors.New("room hard-limit reached (max 50 participants)")
	ErrRoomNotFound        = errors.New("room not found")
	ErrParticipantNotFound = errors.New("participant not found")
)

const MaxRoomParticipants = 50

type Participant struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	JoinedAt       time.Time   `json:"joined_at"`
	IsEgress       bool        `json:"is_egress,omitempty"`
	reconnectTimer *time.Timer `json:"-"`
}

type Room struct {
	Slug         string                  `json:"slug"`
	HostID       string                  `json:"host_id"`
	Participants map[string]*Participant `json:"participants"`

	// Screen-share arbitration state (BR: when multiple participants share
	// simultaneously, the latest one shown by default; others suspended,
	// not dropped; host can pick any active share, including reclaiming
	// their own). activeScreenPublishers tracks every peer currently
	// sharing a screen (peerID -> peerName); activePresentationPeerID is
	// whichever one of those is actually on stage right now, "" if none.
	activeScreenPublishers    map[string]string
	activePresentationPeerID  string
	activePresentationPeerName string

	mu sync.RWMutex
}

type RoomManager struct {
	rooms RWMutexMap
	rdb   redis.Cmdable
}

type RWMutexMap struct {
	sync.RWMutex
	m map[string]*Room
}

func NewRoomManager(rdb redis.Cmdable) *RoomManager {
	return &RoomManager{
		rooms: RWMutexMap{
			m: make(map[string]*Room),
		},
		rdb: rdb,
	}
}

func (rm *RoomManager) CreateOrGetRoom(ctx context.Context, slug string, hostID string) (*Room, error) {
	rm.rooms.Lock()
	defer rm.rooms.Unlock()

	r, exists := rm.rooms.m[slug]
	if !exists {
		r = &Room{
			Slug:                   slug,
			HostID:                 hostID,
			Participants:           make(map[string]*Participant),
			activeScreenPublishers: make(map[string]string),
		}
		rm.rooms.m[slug] = r
	}
	return r, nil
}

func (rm *RoomManager) AddParticipant(ctx context.Context, slug string, p *Participant) error {
	rm.rooms.RLock()
	r, exists := rm.rooms.m[slug]
	rm.rooms.RUnlock()

	if !exists {
		return ErrRoomNotFound
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// A reconnect (same participant ID already present, e.g. a brief network
	// blip during the 15s HandleDisconnect grace window) must never be
	// rejected by the capacity cap — it's not a new occupant, it's replacing
	// its own existing entry. Checking existence before the cap, and
	// exempting egress recorder identities from the cap entirely, are both
	// required or a room sitting at 50 can turn transient disconnects (or a
	// recording session) into unrecoverable "room full" kicks.
	existing, isReconnect := r.Participants[p.ID]

	if !isReconnect && !p.IsEgress && len(r.Participants) >= MaxRoomParticipants {
		return ErrRoomFull
	}

	if isReconnect && existing.reconnectTimer != nil {
		existing.reconnectTimer.Stop()
	}

	r.Participants[p.ID] = p

	// Optional Redis sync if rdb provided
	if rm.rdb != nil {
		rm.rdb.HSet(ctx, "room:"+slug+":participants", p.ID, p.Name)
	}

	return nil
}

// GetRoomHostID returns the room's recorded HostID (the ID passed to
// CreateOrGetRoom when the room was first created) so callers can enforce
// host-only actions against actual room ownership instead of trusting a
// caller's self-declared "host" role claim in isolation.
func (rm *RoomManager) GetRoomHostID(slug string) (string, bool) {
	rm.rooms.RLock()
	defer rm.rooms.RUnlock()

	r, exists := rm.rooms.m[slug]
	if !exists {
		return "", false
	}
	return r.HostID, true
}

// SetScreenSharing records peerID starting or stopping a screen share.
// Starting always makes peerID the active presentation ("latest wins" —
// the default arbitration rule when multiple participants share at once);
// stopping clears the active presentation only if peerID was the one
// actually on stage, leaving any other still-sharing (suspended)
// publishers as they were rather than auto-promoting one of them. Returns
// the room's active presentation after the update ("" peer ID if nobody
// is presenting), and whether this call actually changed anything (so
// callers can skip a redundant broadcast on a no-op, e.g. a participant
// who was never sharing disconnecting).
func (rm *RoomManager) SetScreenSharing(slug, peerID, peerName string, sharing bool) (activePeerID, activePeerName string, changed bool) {
	rm.rooms.RLock()
	r, exists := rm.rooms.m[slug]
	rm.rooms.RUnlock()
	if !exists {
		return "", "", false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.activeScreenPublishers == nil {
		r.activeScreenPublishers = make(map[string]string)
	}

	if sharing {
		_, alreadySharing := r.activeScreenPublishers[peerID]
		r.activeScreenPublishers[peerID] = peerName
		wasActive := r.activePresentationPeerID == peerID
		r.activePresentationPeerID = peerID
		r.activePresentationPeerName = peerName
		changed = !alreadySharing || !wasActive
	} else {
		_, wasSharing := r.activeScreenPublishers[peerID]
		delete(r.activeScreenPublishers, peerID)
		if r.activePresentationPeerID == peerID {
			r.activePresentationPeerID = ""
			r.activePresentationPeerName = ""
			changed = true
		} else if wasSharing {
			changed = true
		}
	}

	return r.activePresentationPeerID, r.activePresentationPeerName, changed
}

// SetActivePresentation is the host override/reclaim path: makes peerID
// the active presentation if — and only if — peerID is currently a known
// screen-sharing publisher in this room, refusing a stale or bogus target
// rather than putting an empty tile on stage. Returns the resulting active
// presentation name and whether the request was valid.
func (rm *RoomManager) SetActivePresentation(slug, peerID string) (activePeerName string, ok bool) {
	rm.rooms.RLock()
	r, exists := rm.rooms.m[slug]
	rm.rooms.RUnlock()
	if !exists {
		return "", false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	name, isSharing := r.activeScreenPublishers[peerID]
	if !isSharing {
		return "", false
	}

	r.activePresentationPeerID = peerID
	r.activePresentationPeerName = name
	return name, true
}

// GetActivePresentation returns the room's current active presentation
// ("" peer ID if nobody is presenting) and whether the room exists.
func (rm *RoomManager) GetActivePresentation(slug string) (peerID, peerName string, ok bool) {
	rm.rooms.RLock()
	r, exists := rm.rooms.m[slug]
	rm.rooms.RUnlock()
	if !exists {
		return "", "", false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activePresentationPeerID, r.activePresentationPeerName, true
}

func (rm *RoomManager) GetParticipant(slug string, participantID string) (*Participant, bool) {
	rm.rooms.RLock()
	r, exists := rm.rooms.m[slug]
	rm.rooms.RUnlock()

	if !exists {
		return nil, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	p, found := r.Participants[participantID]
	return p, found
}

func (rm *RoomManager) GetParticipantCount(slug string) int {
	rm.rooms.RLock()
	r, exists := rm.rooms.m[slug]
	rm.rooms.RUnlock()

	if !exists {
		return 0
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Participants)
}

func (rm *RoomManager) HandleDisconnect(slug string, participantID string, gracePeriod time.Duration) {
	rm.rooms.RLock()
	r, exists := rm.rooms.m[slug]
	rm.rooms.RUnlock()

	if !exists {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	p, found := r.Participants[participantID]
	if !found {
		return
	}

	if p.reconnectTimer != nil {
		p.reconnectTimer.Stop()
	}

	p.reconnectTimer = time.AfterFunc(gracePeriod, func() {
		rm.RemoveParticipant(context.Background(), slug, participantID)
	})
}

func (rm *RoomManager) HandleReconnect(slug string, participantID string) bool {
	rm.rooms.RLock()
	r, exists := rm.rooms.m[slug]
	rm.rooms.RUnlock()

	if !exists {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	p, found := r.Participants[participantID]
	if !found {
		return false
	}

	if p.reconnectTimer != nil {
		p.reconnectTimer.Stop()
		p.reconnectTimer = nil
		return true
	}
	return true
}

func (rm *RoomManager) RemoveParticipant(ctx context.Context, slug string, participantID string) {
	rm.rooms.RLock()
	r, exists := rm.rooms.m[slug]
	rm.rooms.RUnlock()

	if !exists {
		return
	}

	r.mu.Lock()
	if p, found := r.Participants[participantID]; found {
		if p.reconnectTimer != nil {
			p.reconnectTimer.Stop()
		}
		delete(r.Participants, participantID)
	}
	isEmpty := len(r.Participants) == 0
	r.mu.Unlock()

	if rm.rdb != nil {
		rm.rdb.HDel(ctx, "room:"+slug+":participants", participantID)
	}

	// Clean up empty room if needed
	if isEmpty {
		rm.rooms.Lock()
		delete(rm.rooms.m, slug)
		rm.rooms.Unlock()
	}
}
