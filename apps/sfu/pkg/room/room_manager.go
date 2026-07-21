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
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	JoinedAt       time.Time     `json:"joined_at"`
	reconnectTimer *time.Timer   `json:"-"`
}

type Room struct {
	Slug         string                  `json:"slug"`
	HostID       string                  `json:"host_id"`
	Participants map[string]*Participant `json:"participants"`
	mu           sync.RWMutex
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
			Slug:         slug,
			HostID:       hostID,
			Participants: make(map[string]*Participant),
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

	if len(r.Participants) >= MaxRoomParticipants {
		return ErrRoomFull
	}

	// Cancel existing reconnect timer if any
	if existing, found := r.Participants[p.ID]; found {
		if existing.reconnectTimer != nil {
			existing.reconnectTimer.Stop()
		}
	}

	r.Participants[p.ID] = p

	// Optional Redis sync if rdb provided
	if rm.rdb != nil {
		rm.rdb.HSet(ctx, "room:"+slug+":participants", p.ID, p.Name)
	}

	return nil
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
