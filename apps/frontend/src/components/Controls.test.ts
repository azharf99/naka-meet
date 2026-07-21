import { describe, test, expect } from 'vitest';
import { validateRtmpUrl } from './Controls';

describe('Controls RTMP Validation Tests', () => {
  test('validateRtmpUrl accepts valid rtmp and rtmps URLs', () => {
    expect(validateRtmpUrl('rtmp://a.rtmp.youtube.com/live2/key-123')).toBe(true);
    expect(validateRtmpUrl('rtmps://live-api-s.facebook.com:443/rtmp/key-456')).toBe(true);
  });

  test('validateRtmpUrl rejects invalid or empty URLs', () => {
    expect(validateRtmpUrl('')).toBe(false);
    expect(validateRtmpUrl('http://youtube.com/watch?v=123')).toBe(false);
    expect(validateRtmpUrl('invalid-url')).toBe(false);
  });
});
