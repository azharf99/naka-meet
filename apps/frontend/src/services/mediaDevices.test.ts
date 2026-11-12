import { describe, test, expect, beforeEach, vi } from 'vitest';
import {
  STORAGE_KEYS,
  getPreferredDevices,
  setPreferredDevice,
  isSinkIdSupported,
  listMediaDevices,
} from './mediaDevices';

describe('mediaDevices Utility Tests', () => {
  let mockStore: Record<string, string> = {};

  const mockLocalStorage = {
    getItem: (key: string) => (key in mockStore ? mockStore[key] : null),
    setItem: (key: string, value: string) => {
      mockStore[key] = String(value);
    },
    removeItem: (key: string) => {
      delete mockStore[key];
    },
    clear: () => {
      mockStore = {};
    },
  };

  Object.defineProperty(globalThis, 'localStorage', {
    value: mockLocalStorage,
    configurable: true,
    writable: true,
  });

  Object.defineProperty(globalThis, 'window', {
    value: {
      localStorage: mockLocalStorage,
    },
    configurable: true,
    writable: true,
  });

  beforeEach(() => {
    mockStore = {};
    vi.restoreAllMocks();
  });

  test('getPreferredDevices returns empty strings if nothing is stored', () => {
    const prefs = getPreferredDevices();
    expect(prefs).toEqual({ micId: '', speakerId: '', camId: '' });
  });

  test('setPreferredDevice and getPreferredDevices roundtrip properly', () => {
    setPreferredDevice('mic', 'mic-device-123');
    setPreferredDevice('speaker', 'speaker-device-456');
    setPreferredDevice('cam', 'cam-device-789');

    expect(localStorage.getItem(STORAGE_KEYS.MIC)).toBe('mic-device-123');
    expect(localStorage.getItem(STORAGE_KEYS.SPEAKER)).toBe('speaker-device-456');
    expect(localStorage.getItem(STORAGE_KEYS.CAM)).toBe('cam-device-789');

    const prefs = getPreferredDevices();
    expect(prefs).toEqual({
      micId: 'mic-device-123',
      speakerId: 'speaker-device-456',
      camId: 'cam-device-789',
    });

    // Remove preferred device
    setPreferredDevice('mic', '');
    expect(localStorage.getItem(STORAGE_KEYS.MIC)).toBeNull();
  });

  test('listMediaDevices categorizes input and output devices properly', async () => {
    const mockDevices: Partial<MediaDeviceInfo>[] = [
      { deviceId: 'mic-1', kind: 'audioinput', label: 'Default Microphone' },
      { deviceId: 'mic-2', kind: 'audioinput', label: 'Headset Mic' },
      { deviceId: 'speaker-1', kind: 'audiooutput', label: 'Default Speaker' },
      { deviceId: 'cam-1', kind: 'videoinput', label: 'Webcam' },
    ];

    Object.defineProperty(globalThis, 'navigator', {
      value: {
        mediaDevices: {
          enumerateDevices: vi.fn().mockResolvedValue(mockDevices),
        },
      },
      configurable: true,
      writable: true,
    });

    const result = await listMediaDevices();
    expect(result.microphones.length).toBe(2);
    expect(result.speakers.length).toBe(1);
    expect(result.cameras.length).toBe(1);
    expect(result.microphones[0].deviceId).toBe('mic-1');
    expect(result.speakers[0].deviceId).toBe('speaker-1');
    expect(result.cameras[0].deviceId).toBe('cam-1');
  });

  test('isSinkIdSupported checks HTMLMediaElement prototype', () => {
    // In node/vitest, HTMLMediaElement might not have setSinkId unless defined
    expect(typeof isSinkIdSupported()).toBe('boolean');
  });
});
