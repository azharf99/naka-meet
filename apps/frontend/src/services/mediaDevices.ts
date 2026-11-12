// Persistence keys for user-preferred media devices in localStorage
export const STORAGE_KEYS = {
  MIC: 'naka_pref_mic_id',
  SPEAKER: 'naka_pref_speaker_id',
  CAM: 'naka_pref_cam_id',
} as const;

export interface PreferredDevices {
  micId: string;
  speakerId: string;
  camId: string;
}

export interface CategorizedDevices {
  microphones: MediaDeviceInfo[];
  speakers: MediaDeviceInfo[];
  cameras: MediaDeviceInfo[];
}

export function getPreferredDevices(): PreferredDevices {
  if (typeof window === 'undefined' || !window.localStorage) {
    return { micId: '', speakerId: '', camId: '' };
  }
  try {
    return {
      micId: window.localStorage.getItem(STORAGE_KEYS.MIC) || '',
      speakerId: window.localStorage.getItem(STORAGE_KEYS.SPEAKER) || '',
      camId: window.localStorage.getItem(STORAGE_KEYS.CAM) || '',
    };
  } catch {
    return { micId: '', speakerId: '', camId: '' };
  }
}

export function setPreferredDevice(kind: 'mic' | 'speaker' | 'cam', deviceId: string): void {
  if (typeof window === 'undefined' || !window.localStorage) return;
  try {
    const key =
      kind === 'mic'
        ? STORAGE_KEYS.MIC
        : kind === 'speaker'
          ? STORAGE_KEYS.SPEAKER
          : STORAGE_KEYS.CAM;
    if (deviceId) {
      window.localStorage.setItem(key, deviceId);
    } else {
      window.localStorage.removeItem(key);
    }
  } catch (err) {
    console.warn('Failed to save preferred device to localStorage', err);
  }
}

export function isSinkIdSupported(): boolean {
  return (
    typeof HTMLMediaElement !== 'undefined' &&
    'setSinkId' in HTMLMediaElement.prototype
  );
}

export async function listMediaDevices(): Promise<CategorizedDevices> {
  if (
    typeof navigator === 'undefined' ||
    !navigator.mediaDevices ||
    typeof navigator.mediaDevices.enumerateDevices !== 'function'
  ) {
    return { microphones: [], speakers: [], cameras: [] };
  }

  try {
    const devices = await navigator.mediaDevices.enumerateDevices();
    const microphones = devices.filter((d) => d.kind === 'audioinput');
    const speakers = devices.filter((d) => d.kind === 'audiooutput');
    const cameras = devices.filter((d) => d.kind === 'videoinput');

    return { microphones, speakers, cameras };
  } catch (err) {
    console.warn('Could not enumerate media devices:', err);
    return { microphones: [], speakers: [], cameras: [] };
  }
}

/**
 * Plays a pleasant chime sound (sine tone) through the chosen speaker (sinkId)
 * to allow the user to test audio output.
 */
export async function playTestSpeakerTone(sinkId?: string): Promise<void> {
  const AudioCtx = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
  if (!AudioCtx) {
    console.warn('Web Audio API not supported in this browser');
    return;
  }

  const ctx = new AudioCtx();
  // If setSinkId is supported on AudioContext (standard in modern Chromium)
  if (sinkId && typeof (ctx as unknown as { setSinkId?: (id: string) => Promise<void> }).setSinkId === 'function') {
    try {
      await (ctx as unknown as { setSinkId: (id: string) => Promise<void> }).setSinkId(sinkId);
    } catch (e) {
      console.warn('Could not setSinkId on AudioContext, falling back to default output', e);
    }
  }

  const osc = ctx.createOscillator();
  const gain = ctx.createGain();

  const now = ctx.currentTime;
  osc.type = 'sine';
  osc.frequency.setValueAtTime(587.33, now); // D5
  osc.frequency.exponentialRampToValueAtTime(880, now + 0.15); // A5

  gain.gain.setValueAtTime(0.001, now);
  gain.gain.exponentialRampToValueAtTime(0.2, now + 0.05);
  gain.gain.exponentialRampToValueAtTime(0.001, now + 0.45);

  osc.connect(gain);
  gain.connect(ctx.destination);

  osc.start(now);
  osc.stop(now + 0.5);

  setTimeout(() => {
    ctx.close().catch(() => {});
  }, 600);
}
