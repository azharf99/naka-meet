import { describe, test, expect } from 'vitest';
import { getGridClass, deduplicateTracks, getInitials, deriveVisibility } from './VideoGrid';

describe('VideoGrid Helper Tests', () => {
  test('getGridClass assigns dynamic layout based on total participant count', () => {
    expect(getGridClass(1)).toContain('grid-cols-1');
    expect(getGridClass(2)).toContain('grid-cols-1 md:grid-cols-2');
    expect(getGridClass(4)).toContain('grid-cols-2 md:grid-cols-2');
    expect(getGridClass(6)).toContain('grid-cols-2 md:grid-cols-3');
    expect(getGridClass(10)).toContain('grid-cols-2 md:grid-cols-4');
  });

  test('deduplicateTracks consolidates audio & video tracks from same stream/peer into 1 item', () => {
    const tracks = [
      { id: 'v1', peerID: 'peer-1', stream: { id: 'stream-1' } as any, isScreenShare: false },
      { id: 'a1', peerID: 'peer-1', stream: { id: 'stream-1' } as any, isScreenShare: false },
      { id: 'v2', peerID: 'peer-2', stream: { id: 'stream-2' } as any, isScreenShare: false },
    ];
    const deduplicated = deduplicateTracks(tracks);
    expect(deduplicated.length).toBe(2);
    expect(deduplicated[0].peerID).toBe('peer-1');
    expect(deduplicated[1].peerID).toBe('peer-2');
  });

  test('deduplicateTracks preserves isScreenShare flag for BR4 Stage Mode', () => {
    const tracks = [
      { id: 'v1', peerID: 'peer-1', stream: { id: 'stream-1' } as any, isScreenShare: false },
      { id: 's1', peerID: 'peer-1', stream: { id: 'stream-screen' } as any, isScreenShare: true },
    ];
    const deduplicated = deduplicateTracks(tracks);
    const screenTrack = deduplicated.find((t) => t.isScreenShare);
    expect(screenTrack).toBeDefined();
    expect(screenTrack?.isScreenShare).toBe(true);
  });

  test('getInitials calculates Zoom-style avatar initials correctly', () => {
    expect(getInitials('Budi Ganteng')).toBe('BG');
    expect(getInitials('Azhar')).toBe('AZ');
    expect(getInitials('User (You)')).toBe('US');
  });

  describe('deriveVisibility (mute/camera-off state)', () => {
    test('an explicit mute/cam-off signal wins over the track-based heuristic', () => {
      // hasAudio/hasVideo say "on", but the media_state relay says otherwise.
      expect(deriveVisibility(true, true, true, true, true, false)).toEqual({
        showVideoFallback: true,
        showMicMuted: true,
      });
    });

    test('an explicit false signal suppresses the badge even if the heuristic disagrees', () => {
      // hasAudio/hasVideo say "off" (e.g. stream not yet flowing), but an
      // explicit unmuted/camera-on signal has already arrived.
      expect(deriveVisibility(false, false, false, false, true, false)).toEqual({
        showVideoFallback: false,
        showMicMuted: false,
      });
    });

    test('undefined signal (no media_state received yet) falls back to the track-based heuristic', () => {
      expect(deriveVisibility(undefined, undefined, false, false, true, false)).toEqual({
        showVideoFallback: true,
        showMicMuted: true,
      });
      expect(deriveVisibility(undefined, undefined, true, true, true, false)).toEqual({
        showVideoFallback: false,
        showMicMuted: false,
      });
    });

    test('no stream at all always shows the video fallback regardless of any signal', () => {
      expect(deriveVisibility(false, false, true, true, false, false)).toEqual({
        showVideoFallback: true,
        showMicMuted: false,
      });
    });

    test('screen-share tiles never show the video fallback', () => {
      expect(deriveVisibility(undefined, true, false, false, true, true)).toEqual({
        showVideoFallback: false,
        showMicMuted: true,
      });
    });
  });
});



