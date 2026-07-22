import { describe, test, expect, vi, beforeEach } from 'vitest';
import { WebRTCService } from './webrtc';

class MockRTCPeerConnection {
  onicecandidate: ((ev: any) => void) | null = null;
  ontrack: ((ev: any) => void) | null = null;
  ondatachannel: ((ev: any) => void) | null = null;
  onnegotiationneeded: ((ev: any) => void) | null = null;

  signalingState: string = 'stable';

  addTrack = vi.fn();
  addTransceiver = vi.fn();
  createDataChannel = vi.fn().mockReturnValue({
    onmessage: null,
    send: vi.fn(),
    readyState: 'open',
  });
  createOffer = vi.fn().mockResolvedValue({ type: 'offer', sdp: 'mock-sdp' });
  setLocalDescription = vi.fn().mockResolvedValue(undefined);
  setRemoteDescription = vi.fn().mockResolvedValue(undefined);
  createAnswer = vi.fn().mockResolvedValue({ type: 'answer', sdp: 'mock-sdp' });
  addIceCandidate = vi.fn().mockResolvedValue(undefined);
  close = vi.fn();
}

class MockWebSocket {
  static OPEN = 1;
  onopen: (() => void) | null = null;
  onmessage: ((ev: any) => void) | null = null;
  send = vi.fn();
  close = vi.fn();
  readyState: number = 1; // WebSocket.OPEN
}

class MockMediaStream {
  id: string;
  tracks: any[];
  constructor(tracks: any[] = []) {
    this.id = 'stream-' + Math.random().toString(36).substring(7);
    this.tracks = tracks;
  }
  getVideoTracks() {
    return this.tracks.filter(t => t.kind === 'video');
  }
  getAudioTracks() {
    return this.tracks.filter(t => t.kind === 'audio');
  }
  getTracks() {
    return this.tracks;
  }
  addTrack(track: any) {
    if (!this.tracks.includes(track)) {
      this.tracks.push(track);
    }
  }
}

describe('WebRTC Service Renegotiation and Ontrack Registration Tests', () => {
  beforeEach(() => {
    vi.stubGlobal('RTCPeerConnection', MockRTCPeerConnection);
    vi.stubGlobal('WebSocket', MockWebSocket);
    vi.stubGlobal('MediaStream', MockMediaStream);
    vi.stubGlobal('RTCSessionDescription', vi.fn((desc) => desc));
    vi.stubGlobal('RTCIceCandidate', vi.fn((cand) => cand));
    vi.stubGlobal('navigator', {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue({
          getTracks: () => [],
        }),
      },
    });
  });

  test('Polite peer rolls back local description during remote offer glare', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    const pc = (service as any).pc as any;
    const ws = (service as any).ws as any;

    // Simulate glare by changing signalingState to non-stable (like 'have-local-offer')
    pc.signalingState = 'have-local-offer';

    // Simulate receiving remote offer
    if (ws.onmessage) {
      await ws.onmessage({
        data: JSON.stringify({
          type: 'offer',
          sdp: 'remote-sdp-glare',
        }),
      });
    }

    // Verify rollback was triggered
    expect(pc.setLocalDescription).toHaveBeenCalledWith({ type: 'rollback' });
    expect(pc.setRemoteDescription).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'offer', sdp: 'remote-sdp-glare' })
    );
  });

  test('ontrack fallback maps empty streams using metadata map', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    const pc = (service as any).pc as any;
    const ws = (service as any).ws as any;

    const addedTracks: any[] = [];
    service.onTrackAdded = (track) => {
      addedTracks.push(track);
    };

    // 1. Send track_metadata mapping track-abc to stream-xyz
    if (ws.onmessage) {
      ws.onmessage({
        data: JSON.stringify({
          type: 'track_metadata',
          stream_id: 'stream-xyz',
          track_id: 'track-abc',
          peer_id: 'peer-1',
          peer_name: 'Budi',
          kind: 'camera',
        }),
      });
    }

    // 2. Trigger ontrack with EMPTY event.streams array
    if (pc && pc.ontrack) {
      pc.ontrack({
        track: { id: 'track-abc', label: 'camera-video', kind: 'video' },
        streams: [], // Empty streams array
      });
    }

    expect(addedTracks.length).toBe(1);
    expect(addedTracks[0].peerID).toBe('Budi');
    expect(addedTracks[0].isScreenShare).toBe(false);
    expect(addedTracks[0].stream).toBeDefined();
    expect(addedTracks[0].stream.id).toBe('stream-xyz');
  });

  test('WebSocket onopen triggers initial SDP offer even when localStream has 0 tracks', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    const pc = (service as any).pc as any;
    const ws = (service as any).ws as any;

    expect(ws.onopen).toBeDefined();

    // Trigger onopen
    if (ws.onopen) {
      await ws.onopen();
    }

    expect(pc.createOffer).toHaveBeenCalled();
    expect(ws.send).toHaveBeenCalledWith(
      JSON.stringify({ type: 'offer', sdp: 'mock-sdp' })
    );
  });
});
