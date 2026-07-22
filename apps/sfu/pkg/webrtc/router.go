package webrtc

import (
	"fmt"
	"sync"

	pion "github.com/pion/webrtc/v4"
)

type RoomTrack struct {
	PublisherID   string
	PublisherName string
	Kind          string
	Track         pion.TrackLocal
}

type SFURouter struct {
	api        *pion.API
	peers      map[string]*pion.PeerConnection
	roomTracks map[string][]*RoomTrack
	peerRooms  map[string]string
	mu         sync.RWMutex
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
	}, nil
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
	return pc, nil
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
	r.mu.Unlock()

	r.mu.RLock()
	defer r.mu.RUnlock()

	for peerID, pc := range r.peers {
		if peerID == publisherID {
			continue
		}
		if r.peerRooms[peerID] != roomSlug {
			continue
		}

		if _, err := pc.AddTrack(track); err == nil {
			if pc.SignalingState() == pion.SignalingStateStable {
				offer, err := pc.CreateOffer(nil)
				if err == nil {
					if err := pc.SetLocalDescription(offer); err == nil {
						sendOffer(peerID, offer.SDP)
					}
				}
			}
		}
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

