package webrtc

import (
	"fmt"
	"sync"
	"time"

	pion "github.com/pion/webrtc/v4"
)

// renegotiationStableTimeout bounds how long a renegotiation attempt will
// wait for a peer's PeerConnection to return to a stable signaling state
// (e.g. while the browser is still answering a prior offer) before giving
// up. Real round trips complete in well under a second.
const renegotiationStableTimeout = 3 * time.Second

type RoomTrack struct {
	PublisherID   string
	PublisherName string
	Kind          string
	Track         pion.TrackLocal
}

type SFURouter struct {
	api         *pion.API
	peers       map[string]*pion.PeerConnection
	roomTracks  map[string][]*RoomTrack
	peerRooms   map[string]string
	negoLocks   map[string]*sync.Mutex
	offerSender func(peerID, sdp string)
	mu          sync.RWMutex
}

func NewSFURouter(udpMin, udpMax uint16) (*SFURouter, error) {
	settingEngine := pion.SettingEngine{}
	if err := settingEngine.SetEphemeralUDPPortRange(udpMin, udpMax); err != nil {
		return nil, fmt.Errorf("failed to set UDP port range (%d-%d): %w", udpMin, udpMax, err)
	}

	mediaEngine := &pion.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("failed to register default codecs: %w", err)
	}

	api := pion.NewAPI(
		pion.WithSettingEngine(settingEngine),
		pion.WithMediaEngine(mediaEngine),
	)

	return &SFURouter{
		api:        api,
		peers:      make(map[string]*pion.PeerConnection),
		roomTracks: make(map[string][]*RoomTrack),
		peerRooms:  make(map[string]string),
		negoLocks:  make(map[string]*sync.Mutex),
	}, nil
}

// SetOfferSender registers the default transport used to deliver a
// server-initiated renegotiation offer to a peer once its signaling state
// returns to stable. Called once by the signaling handler at startup.
func (r *SFURouter) SetOfferSender(fn func(peerID, sdp string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offerSender = fn
}

func (r *SFURouter) AddPeer(peerID string) (*pion.PeerConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if pc, exists := r.peers[peerID]; exists {
		return pc, nil
	}

	config := pion.Configuration{
		ICEServers: []pion.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	pc, err := r.api.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create PeerConnection for %s: %w", peerID, err)
	}

	r.peers[peerID] = pc
	r.negoLocks[peerID] = &sync.Mutex{}
	return pc, nil
}

// RenegotiatePeer triggers a serialized renegotiation attempt for peerID,
// picking up any tracks that were added to its PeerConnection but not yet
// offered. Delivery uses the router's default offer sender.
func (r *SFURouter) RenegotiatePeer(peerID string) {
	r.renegotiatePeer(peerID, nil)
}

// renegotiatePeer serializes CreateOffer/SetLocalDescription per target peer
// so concurrent callers targeting the same peer can never race on its
// PeerConnection's signaling state (the classic cause of a track silently
// never getting offered: two tracks from the same publisher, or two
// publishers joining at once, each try to renegotiate the same subscriber's
// PeerConnection at nearly the same instant). Renegotiations for different
// peers proceed independently. If the peer isn't stable right now — most
// commonly because it's still waiting on the browser's answer to a previous
// offer — this waits (bounded by renegotiationStableTimeout) instead of
// dropping the attempt, since that in-flight negotiation is expected to
// resolve within a normal network round trip.
func (r *SFURouter) renegotiatePeer(peerID string, sender func(peerID, sdp string)) {
	r.mu.RLock()
	pc, exists := r.peers[peerID]
	lock := r.negoLocks[peerID]
	if sender == nil {
		sender = r.offerSender
	}
	r.mu.RUnlock()

	if !exists || pc == nil || lock == nil {
		return
	}

	lock.Lock()
	defer lock.Unlock()

	deadline := time.Now().Add(renegotiationStableTimeout)
	for pc.SignalingState() != pion.SignalingStateStable {
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return
	}
	if sender != nil {
		sender(peerID, offer.SDP)
	}
}

func (r *SFURouter) GetPeer(peerID string) (*pion.PeerConnection, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pc, exists := r.peers[peerID]
	return pc, exists
}

func (r *SFURouter) SetPeerRoom(peerID string, roomSlug string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.peerRooms == nil {
		r.peerRooms = make(map[string]string)
	}
	r.peerRooms[peerID] = roomSlug
}

func (r *SFURouter) RemovePeer(peerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	pc, exists := r.peers[peerID]
	if exists {
		delete(r.peers, peerID)
		_ = pc.Close()
	}

	if r.peerRooms != nil {
		delete(r.peerRooms, peerID)
	}
	delete(r.negoLocks, peerID)

	// Clean up any tracks published by this peer across all rooms
	for slug, tracks := range r.roomTracks {
		var active []*RoomTrack
		for _, rt := range tracks {
			if rt.PublisherID != peerID {
				active = append(active, rt)
			}
		}
		r.roomTracks[slug] = active
	}

	return nil
}

func (r *SFURouter) AddTrackToRoom(roomSlug, publisherID string, track pion.TrackLocal) {
	r.AddTrackToRoomWithMetadata(roomSlug, publisherID, "", "camera", track)
}

func (r *SFURouter) AddTrackToRoomWithMetadata(roomSlug, publisherID, publisherName, kind string, track pion.TrackLocal) {
	r.mu.Lock()
	rt := &RoomTrack{
		PublisherID:   publisherID,
		PublisherName: publisherName,
		Kind:          kind,
		Track:         track,
	}
	r.roomTracks[roomSlug] = append(r.roomTracks[roomSlug], rt)
	r.mu.Unlock()

	_, _ = r.BroadcastTrack(publisherID, track)
}

func (r *SFURouter) BroadcastTrackAndRenegotiate(roomSlug, publisherID string, track pion.TrackLocal, sendOffer func(peerID, offerSDP string)) {
	r.BroadcastTrackAndRenegotiateWithMetadata(roomSlug, publisherID, "", "camera", track, sendOffer)
}

func (r *SFURouter) BroadcastTrackAndRenegotiateWithMetadata(roomSlug, publisherID, publisherName, kind string, track pion.TrackLocal, sendOffer func(peerID, offerSDP string)) {
	r.mu.Lock()
	rt := &RoomTrack{
		PublisherID:   publisherID,
		PublisherName: publisherName,
		Kind:          kind,
		Track:         track,
	}
	r.roomTracks[roomSlug] = append(r.roomTracks[roomSlug], rt)

	var targetIDs []string
	for peerID := range r.peers {
		if peerID == publisherID {
			continue
		}
		if r.peerRooms[peerID] != roomSlug {
			continue
		}
		targetIDs = append(targetIDs, peerID)
	}
	r.mu.Unlock()

	// AddTrack + renegotiation happens outside the map lock, and
	// renegotiatePeer serializes negotiation per target peer, so concurrent
	// broadcasts (e.g. camera + mic tracks published back-to-back) can never
	// race on the same target PeerConnection's signaling state and silently
	// drop an offer.
	for _, peerID := range targetIDs {
		r.mu.RLock()
		pc, exists := r.peers[peerID]
		r.mu.RUnlock()
		if !exists {
			continue
		}
		if _, err := pc.AddTrack(track); err != nil {
			continue
		}
		r.renegotiatePeer(peerID, sendOffer)
	}
}

func (r *SFURouter) GetRoomTracks(roomSlug string) []*RoomTrack {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tracks := r.roomTracks[roomSlug]
	res := make([]*RoomTrack, len(tracks))
	copy(res, tracks)
	return res
}

func (r *SFURouter) SubscribePeerToRoomTracks(roomSlug, peerID string, onSubscribe func(rt *RoomTrack)) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pc, exists := r.peers[peerID]
	if !exists {
		return 0, fmt.Errorf("peer %s not found", peerID)
	}

	tracks := r.roomTracks[roomSlug]
	count := 0
	for _, rt := range tracks {
		if rt.PublisherID == peerID {
			continue
		}
		if _, err := pc.AddTrack(rt.Track); err == nil {
			if onSubscribe != nil {
				onSubscribe(rt)
			}
			count++
		}
	}
	return count, nil
}

func (r *SFURouter) BroadcastTrack(publisherID string, track pion.TrackLocal) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	pubRoom := r.peerRooms[publisherID]

	for peerID, pc := range r.peers {
		if peerID == publisherID {
			continue
		}
		if pubRoom != "" && r.peerRooms[peerID] != "" && r.peerRooms[peerID] != pubRoom {
			continue
		}

		if _, err := pc.AddTrack(track); err != nil {
			return count, fmt.Errorf("failed to add track to subscriber %s: %w", peerID, err)
		}
		count++
	}

	return count, nil
}

