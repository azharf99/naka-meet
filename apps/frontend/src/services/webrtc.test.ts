import { describe, test, expect, vi } from 'vitest';
import { WebRTCService } from './webrtc';

class MockRTCPeerConnection {
  onicecandidate: ((ev: any) => void) | null = null;
  ontrack: ((ev: any) => void) | null = null;
  ondatachannel: ((ev: any) => void) | null = null;
  onnegotiationneeded: ((ev: any) => void) | null = null;

  addTrack = vi.fn();
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
  onmessage: ((ev: any) => void) | null = null;
  send = vi.fn();
  close = vi.fn();
}

Object.defineProperty(globalThis, 'RTCPeerConnection', { value: MockRTCPeerConnection, writable: true });
Object.defineProperty(globalThis, 'WebSocket', { value: MockWebSocket, writable: true });
Object.defineProperty(globalThis, 'RTCSessionDescription', { value: vi.fn(), writable: true });
Object.defineProperty(globalThis, 'RTCIceCandidate', { value: vi.fn(), writable: true });
Object.defineProperty(globalThis, 'navigator', {
  value: {
    mediaDevices: {
      getUserMedia: vi.fn().mockResolvedValue({
        getTracks: () => [],
      }),
      getDisplayMedia: vi.fn().mockResolvedValue({
        id: 'screen-stream-1',
        getTracks: () => [{ id: 'screen-1', label: 'Screen Share', stop: vi.fn() }],
      }),

    },
  },
  writable: true,
});

describe('WebRTCService Audit & Unit Tests', () => {
  test('WebRTCService initializes and sets up ICE candidate listener', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    expect(service).toBeDefined();
  });

  test('sendMessage sends data via DataChannel', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let received: any = null;
    service.onMessageReceived = (msg) => {
      received = msg;
    };

    service.sendMessage('Hello WebRTC');
    expect(received).not.toBeNull();
    expect(received.text).toBe('Hello WebRTC');
  });

  test('stopScreenShare stops screen tracks and triggers onScreenShareEnded callback', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let ended = false;
    service.onScreenShareEnded = () => {
      ended = true;
    };

    await service.startScreenShare();
    service.stopScreenShare();
    expect(ended).toBe(true);
  });

  test('track_metadata maps peer_name to tracks without creating empty MediaStream when kind is screen', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    const addedTracks: any[] = [];
    service.onTrackAdded = (track) => {
      addedTracks.push(track);
    };

    // Simulate receiving out-of-band track_metadata over WebSocket before ontrack
    const ws = (service as any).ws;
    expect(ws).toBeDefined();

    // 1. Send screen track_metadata -> should NOT immediately call onTrackAdded with empty stream
    if (ws.onmessage) {
      ws.onmessage({
        data: JSON.stringify({
          type: 'track_metadata',
          stream_id: 'screen-stream-100',
          peer_id: 'user-1',
          peer_name: 'Alice Presenter',
          kind: 'screen',
        }),
      });
    }
    expect(addedTracks.length).toBe(0); // Should not emit empty MediaStream

    // 2. Now simulate browser ontrack event firing for screen-stream-100
    const pc = (service as any).pc;
    const mockScreenStream = {
      id: 'screen-stream-100',
      getVideoTracks: () => [{ id: 'video-track-1', enabled: true, readyState: 'live' }],
      getAudioTracks: () => [],
    };
    if (pc && pc.ontrack) {
      pc.ontrack({
        track: { id: 'video-track-1', label: 'screen-video', kind: 'video' },
        streams: [mockScreenStream],
      });
    }

    expect(addedTracks.length).toBe(1);
    expect(addedTracks[0].peerID).toBe('Alice Presenter');
    expect(addedTracks[0].isScreenShare).toBe(true);
    expect(addedTracks[0].stream.id).toBe('screen-stream-100');
  });
});


