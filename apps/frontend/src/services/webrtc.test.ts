import { describe, test, expect, vi } from 'vitest';
import { WebRTCService } from './webrtc';

class MockRTCPeerConnection {
  onicecandidate: ((ev: any) => void) | null = null;
  ontrack: ((ev: any) => void) | null = null;
  onnegotiationneeded: ((ev: any) => void) | null = null;

  addTrack = vi.fn();
  createOffer = vi.fn().mockResolvedValue({ type: 'offer', sdp: 'mock-sdp' });
  setLocalDescription = vi.fn().mockResolvedValue(undefined);
  setRemoteDescription = vi.fn().mockResolvedValue(undefined);
  createAnswer = vi.fn().mockResolvedValue({ type: 'answer', sdp: 'mock-sdp' });
  addIceCandidate = vi.fn().mockResolvedValue(undefined);
  close = vi.fn();
}

class MockWebSocket {
  static OPEN = 1;
  onmessage: ((ev: any) => void) | null = null;
  send = vi.fn();
  close = vi.fn();
  readyState: number = 1; // WebSocket.OPEN
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

  test('sendMessage relays chat via the signaling WebSocket and echoes it locally', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let received: any = null;
    service.onMessageReceived = (msg) => {
      received = msg;
    };

    const ws = (service as any).ws;
    service.sendMessage('Hello WebRTC');

    expect(ws.send).toHaveBeenCalledWith(JSON.stringify({ type: 'chat', text: 'Hello WebRTC' }));
    expect(received).not.toBeNull();
    expect(received.text).toBe('Hello WebRTC');
    expect(received.sender).toBe('You');
  });

  test('incoming chat message from another participant triggers onMessageReceived', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let received: any = null;
    service.onMessageReceived = (msg) => {
      received = msg;
    };

    const ws = (service as any).ws;
    if (ws.onmessage) {
      ws.onmessage({
        data: JSON.stringify({ type: 'chat', text: 'Hi there', sender: 'Budi', time: '10:00' }),
      });
    }

    expect(received).toEqual({ sender: 'Budi', text: 'Hi there', time: '10:00' });
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

  test('sendMediaState sends the exact expected media_state payload', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    const ws = (service as any).ws;
    service.sendMediaState('mic', false);

    expect(ws.send).toHaveBeenCalledWith(
      JSON.stringify({ type: 'media_state', media_kind: 'mic', enabled: false })
    );
  });

  test('incoming media_state prefers peer_name over peer_id for onMediaStateChanged, matching onTrackAdded convention', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let received: any = null;
    service.onMediaStateChanged = (state) => {
      received = state;
    };

    const ws = (service as any).ws;
    ws.onmessage({
      data: JSON.stringify({
        type: 'media_state',
        media_kind: 'cam',
        enabled: false,
        peer_id: 'user-uuid-1',
        peer_name: 'Bob',
      }),
    });

    expect(received).toEqual({ peerId: 'Bob', kind: 'cam', enabled: false });
  });

  test('incoming media_state falls back to peer_id when peer_name is absent', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let received: any = null;
    service.onMediaStateChanged = (state) => {
      received = state;
    };

    const ws = (service as any).ws;
    ws.onmessage({
      data: JSON.stringify({
        type: 'media_state',
        media_kind: 'mic',
        enabled: true,
        peer_id: 'user-uuid-2',
      }),
    });

    expect(received).toEqual({ peerId: 'user-uuid-2', kind: 'mic', enabled: true });
  });

  test('incoming peer_stale prefers peer_name over peer_id, matching media_state convention', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let received: any = null;
    service.onPeerStaleChanged = (state) => {
      received = state;
    };

    const ws = (service as any).ws;
    ws.onmessage({
      data: JSON.stringify({
        type: 'peer_stale',
        peer_id: 'user-uuid-3',
        peer_name: 'Charlie',
        kind: 'camera',
        stale: true,
      }),
    });

    expect(received).toEqual({ peerId: 'Charlie', kind: 'camera', stale: true });
  });

  test('incoming removed message triggers onRemoved and suppresses the generic onDisconnected on close', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let removedReason: string | null = null;
    let disconnectedCalled = false;
    service.onRemoved = (reason) => {
      removedReason = reason;
    };
    service.onDisconnected = () => {
      disconnectedCalled = true;
    };

    const ws = (service as any).ws;
    ws.onmessage({
      data: JSON.stringify({ type: 'removed', reason: 'host_removed' }),
    });
    expect(removedReason).toBe('host_removed');

    // The server closes the socket right after sending "removed" — that
    // close must not also trigger the generic disconnected banner.
    if (ws.onclose) ws.onclose();
    expect(disconnectedCalled).toBe(false);
  });

  test('incoming force_mute message triggers onForceMuted (BR1: host mutes another participant)', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let forceMutedCalled = false;
    service.onForceMuted = () => {
      forceMutedCalled = true;
    };

    const ws = (service as any).ws;
    ws.onmessage({ data: JSON.stringify({ type: 'force_mute' }) });

    expect(forceMutedCalled).toBe(true);
  });

  test('incoming presentation_state triggers onPresentationStateChanged with raw peer id/name', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let received: any = null;
    service.onPresentationStateChanged = (state) => {
      received = state;
    };

    const ws = (service as any).ws;
    ws.onmessage({
      data: JSON.stringify({
        type: 'presentation_state',
        active_peer_id: 'user-uuid-4',
        active_peer_name: 'Dana',
      }),
    });

    expect(received).toEqual({ activePeerId: 'user-uuid-4', activePeerName: 'Dana' });
  });

  test('incoming presentation_state with no active presenter reports an empty peer id', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let received: any = null;
    service.onPresentationStateChanged = (state) => {
      received = state;
    };

    const ws = (service as any).ws;
    ws.onmessage({
      data: JSON.stringify({ type: 'presentation_state', active_peer_id: '', active_peer_name: '' }),
    });

    expect(received).toEqual({ activePeerId: '', activePeerName: '' });
  });

  test('setPresentation sends the host-override request with the target peer id', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    const ws = (service as any).ws;
    service.setPresentation('user-uuid-5');

    expect(ws.send).toHaveBeenCalledWith(JSON.stringify({ type: 'set_presentation', peer_id: 'user-uuid-5' }));
  });

  test('setPresentation is a no-op without a peer id', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    const ws = (service as any).ws;
    service.setPresentation('');

    expect(ws.send).not.toHaveBeenCalled();
  });

  test('incoming recording_state triggers onRecordingStateChanged for every participant', async () => {
    const service = new WebRTCService('demo-room');
    await service.connectToken('mock-jwt-token');

    let received: any = null;
    service.onRecordingStateChanged = (state) => {
      received = state;
    };

    const ws = (service as any).ws;
    ws.onmessage({
      data: JSON.stringify({ type: 'recording_state', active: true, kind: 'recording' }),
    });

    expect(received).toEqual({ active: true, kind: 'recording' });
  });
});


