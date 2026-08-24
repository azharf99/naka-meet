import { describe, test, expect } from 'vitest';
import {
  getGridClass,
  deduplicateTracks,
  getInitials,
  deriveVisibility,
  getTotalTileCount,
  resolvePresentationView,
  getSidebarLayoutClasses,
} from './VideoGrid';
import type { ParticipantTrack } from '../services/webrtc';

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
      { id: 'v1', peerID: 'peer-1', rawPeerId: 'uuid-1', stream: { id: 'stream-1' } as any, isScreenShare: false },
      { id: 'a1', peerID: 'peer-1', rawPeerId: 'uuid-1', stream: { id: 'stream-1' } as any, isScreenShare: false },
      { id: 'v2', peerID: 'peer-2', rawPeerId: 'uuid-2', stream: { id: 'stream-2' } as any, isScreenShare: false },
    ];
    const deduplicated = deduplicateTracks(tracks);
    expect(deduplicated.length).toBe(2);
    expect(deduplicated[0].peerID).toBe('peer-1');
    expect(deduplicated[1].peerID).toBe('peer-2');
  });

  test('deduplicateTracks preserves isScreenShare flag for BR4 Stage Mode', () => {
    const tracks = [
      { id: 'v1', peerID: 'peer-1', rawPeerId: 'uuid-1', stream: { id: 'stream-1' } as any, isScreenShare: false },
      { id: 's1', peerID: 'peer-1', rawPeerId: 'uuid-1', stream: { id: 'stream-screen' } as any, isScreenShare: true },
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

  describe('getTotalTileCount (egress shadow-tile fix)', () => {
    test('counts a local self-tile for a normal participant', () => {
      expect(getTotalTileCount(3, false)).toBe(4);
      expect(getTotalTileCount(0, false)).toBe(1);
    });

    test('excludes the local self-tile for the headless egress recorder, which has no real camera', () => {
      expect(getTotalTileCount(3, true)).toBe(3);
      expect(getTotalTileCount(0, true)).toBe(0);
    });
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

  describe('resolvePresentationView (screen-share arbitration)', () => {
    const aliceScreen: ParticipantTrack = {
      id: 'alice-screen-track',
      peerID: 'Alice',
      rawPeerId: 'alice-uuid',
      stream: { id: 'alice-screen-stream' } as any,
      isScreenShare: true,
    };
    const bobScreen: ParticipantTrack = {
      id: 'bob-screen-track',
      peerID: 'Bob',
      rawPeerId: 'bob-uuid',
      stream: { id: 'bob-screen-stream' } as any,
      isScreenShare: true,
    };
    const carolCamera: ParticipantTrack = {
      id: 'carol-cam-track',
      peerID: 'Carol',
      rawPeerId: 'carol-uuid',
      stream: { id: 'carol-cam-stream' } as any,
      isScreenShare: false,
    };

    test('nobody presenting: Stage Mode stays off', () => {
      const view = resolvePresentationView([], 'host-uuid', false, '', '');
      expect(view.stageModeActive).toBe(false);
      expect(view.activeIsLocal).toBe(false);
      expect(view.activeRemoteTrack).toBeUndefined();
    });

    test('a remote share is active: resolves the matching track and label', () => {
      const view = resolvePresentationView([aliceScreen, carolCamera], 'host-uuid', false, 'alice-uuid', 'Alice');
      expect(view.stageModeActive).toBe(true);
      expect(view.activeIsLocal).toBe(false);
      expect(view.activeRemoteTrack).toBe(aliceScreen);
      expect(view.presentationLabel).toBe("Alice's Presentation");
      expect(view.suspendedRemoteScreens).toEqual([]);
      expect(view.localSuspended).toBe(false);
    });

    test('the local user is the active presenter', () => {
      const view = resolvePresentationView([carolCamera], 'host-uuid', true, 'host-uuid', 'Host');
      expect(view.activeIsLocal).toBe(true);
      expect(view.activeRemoteTrack).toBeUndefined();
      expect(view.presentationLabel).toBe('Your Screen Presentation');
      expect(view.localSuspended).toBe(false);
    });

    test('a later remote share suspends an earlier one — not dropped, just not active', () => {
      // Bob's share is active; Alice's is still live but suspended.
      const view = resolvePresentationView([aliceScreen, bobScreen], 'host-uuid', false, 'bob-uuid', 'Bob');
      expect(view.activeRemoteTrack).toBe(bobScreen);
      expect(view.suspendedRemoteScreens).toEqual([aliceScreen]);
    });

    test('the local user is suspended by a remote share', () => {
      const view = resolvePresentationView([aliceScreen], 'host-uuid', true, 'alice-uuid', 'Alice');
      expect(view.activeIsLocal).toBe(false);
      expect(view.localSuspended).toBe(true);
      expect(view.stageModeActive).toBe(true);
    });

    test('Stage Mode stays active for a suspended local share even with no remote match yet', () => {
      // activePeerId is someone else entirely (or stale/transient) but the
      // local user is still presenting — must not silently fall back to
      // the plain grid mid-suspension.
      const view = resolvePresentationView([], 'host-uuid', true, 'someone-else', 'Someone');
      expect(view.stageModeActive).toBe(true);
      expect(view.localSuspended).toBe(true);
      expect(view.presentationLabel).toBe('Presentation Screen');
    });
  });

  describe('getSidebarLayoutClasses (Stage Mode sidebar scroll-vs-shrink)', () => {
    test('the egress recorder keeps the shrink-to-fit, no-scroll layout', () => {
      // Its viewport can never scroll (FFmpeg only ever captures whatever's
      // currently visible), so every tile must stay on screen, however small.
      const layout = getSidebarLayoutClasses(true);
      expect(layout.container).toContain('overflow-hidden');
      expect(layout.container).not.toContain('overflow-y-auto');
      expect(layout.tile).toContain('min-h-0');
    });

    test('a real user-facing UI scrolls instead of shrinking tiles past legibility', () => {
      const layout = getSidebarLayoutClasses(false);
      expect(layout.container).toContain('overflow-y-auto');
      expect(layout.container).not.toContain('overflow-hidden');
      expect(layout.tile).not.toContain('min-h-0');
    });
  });
});



