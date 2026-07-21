import { describe, test, expect } from 'vitest';
import { getGridClass, deduplicateTracks } from './VideoGrid';

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
});

