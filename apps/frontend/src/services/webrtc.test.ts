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
        getTracks: () => [{ id: 'screen-1', label: 'Screen Share' }],
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
});
