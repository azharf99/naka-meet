package webrtc

import (
	"fmt"
	"sync"

	pion "github.com/pion/webrtc/v4"
)

type SFURouter struct {
	api        *pion.API
	peers      map[string]*pion.PeerConnection
	roomTracks map[string][]pion.TrackLocal
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
		roomTracks: make(map[string][]pion.TrackLocal),
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

func (r *SFURouter) RemovePeer(peerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	pc, exists := r.peers[peerID]
	if !exists {
		return nil
	}

	delete(r.peers, peerID)
	return pc.Close()
}

func (r *SFURouter) AddTrackToRoom(roomSlug, publisherID string, track pion.TrackLocal) {
	r.mu.Lock()
	r.roomTracks[roomSlug] = append(r.roomTracks[roomSlug], track)
	r.mu.Unlock()

	_, _ = r.BroadcastTrack(publisherID, track)
}

func (r *SFURouter) BroadcastTrackAndRenegotiate(roomSlug, publisherID string, track pion.TrackLocal, sendOffer func(peerID, offerSDP string)) {
	r.mu.Lock()
	r.roomTracks[roomSlug] = append(r.roomTracks[roomSlug], track)
	r.mu.Unlock()

	r.mu.RLock()
	defer r.mu.RUnlock()

	for peerID, pc := range r.peers {
		if peerID == publisherID {
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

func (r *SFURouter) SubscribePeerToRoomTracks(roomSlug, peerID string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pc, exists := r.peers[peerID]
	if !exists {
		return 0, fmt.Errorf("peer %s not found", peerID)
	}

	tracks := r.roomTracks[roomSlug]
	count := 0
	for _, track := range tracks {
		if _, err := pc.AddTrack(track); err == nil {
			count++
		}
	}
	return count, nil
}

func (r *SFURouter) BroadcastTrack(publisherID string, track pion.TrackLocal) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for peerID, pc := range r.peers {
		if peerID == publisherID {
			continue
		}

		if _, err := pc.AddTrack(track); err != nil {
			return count, fmt.Errorf("failed to add track to subscriber %s: %w", peerID, err)
		}
		count++
	}

	return count, nil
}
