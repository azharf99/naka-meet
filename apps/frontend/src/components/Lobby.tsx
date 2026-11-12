import React, { useEffect, useState } from 'react';
import { Users, PlusCircle, Sparkles, LogIn, ShieldCheck, ArrowRight, AlertCircle, UserPlus, LogOut, Mic, MicOff, Video, VideoOff, Settings, Volume2, X } from 'lucide-react';
import { login, signup, createRoom, logout, SessionResponse } from '../services/auth';
import {
  CategorizedDevices,
  getPreferredDevices,
  setPreferredDevice,
  listMediaDevices,
  playTestSpeakerTone,
  isSinkIdSupported,
} from '../services/mediaDevices';

export function validateJoinInput(name: string, roomSlug: string): { valid: boolean; error?: string } {
  if (!name.trim()) return { valid: false, error: 'Display name is required' };
  if (!roomSlug.trim()) return { valid: false, error: 'Room name is required' };
  return { valid: true };
}

export function shouldAutoJoinEgress(role: string, roomSlug: string): boolean {
  return role === 'egress' && !!roomSlug.trim();
}

export type HostSession = Pick<SessionResponse, 'token' | 'name' | 'role' | 'user_id'>;

export interface JoinMediaOptions {
  micOn: boolean;
  camOn: boolean;
  audioDeviceId?: string;
  videoDeviceId?: string;
  speakerDeviceId?: string;
}

interface LobbyProps {
  initialRoomSlug?: string;
  initialHostSession?: HostSession | null;
  onJoinRoom: (name: string, roomSlug: string, role: string, options?: JoinMediaOptions) => void;
  onEnterAsHost: (session: HostSession, roomSlug: string, options?: JoinMediaOptions) => void;
}


export const Lobby: React.FC<LobbyProps> = ({ initialRoomSlug = '', initialHostSession = null, onJoinRoom, onEnterAsHost }) => {
  const [guestName, setGuestName] = useState('');
  const [guestRoomSlug, setGuestRoomSlug] = useState(initialRoomSlug || 'demo-room');
  const [guestError, setGuestError] = useState('');
  const [micOn, setMicOn] = useState(true);
  const [camOn, setCamOn] = useState(true);

  const initialPreferred = getPreferredDevices();
  const [selectedMicId, setSelectedMicId] = useState(initialPreferred.micId);
  const [selectedSpeakerId, setSelectedSpeakerId] = useState(initialPreferred.speakerId);
  const [selectedCamId, setSelectedCamId] = useState(initialPreferred.camId);
  const [showDeviceSettings, setShowDeviceSettings] = useState(false);
  const [devices, setDevices] = useState<CategorizedDevices>({ microphones: [], speakers: [], cameras: [] });
  const [isPlayingTest, setIsPlayingTest] = useState(false);

  const [authMode, setAuthMode] = useState<'login' | 'signup'>('login');
  const [hostSession, setHostSession] = useState<HostSession | null>(initialHostSession);
  const [prevInitialSession, setPrevInitialSession] = useState<HostSession | null>(initialHostSession);
  const [hostName, setHostName] = useState('');
  const [hostEmail, setHostEmail] = useState('');
  const [hostPassword, setHostPassword] = useState('');
  const [hostRoomSlug, setHostRoomSlug] = useState('');
  const [hostError, setHostError] = useState('');
  const [hostBusy, setHostBusy] = useState(false);

  if (initialHostSession !== prevInitialSession) {
    setPrevInitialSession(initialHostSession);
    setHostSession(initialHostSession);
  }

  // Enumerate connected devices and listen for devicechange events
  useEffect(() => {
    let mounted = true;
    const updateDevices = () => {
      listMediaDevices().then((res) => {
        if (mounted) setDevices(res);
      });
    };

    updateDevices();

    if (typeof navigator !== 'undefined' && navigator.mediaDevices?.addEventListener) {
      navigator.mediaDevices.addEventListener('devicechange', updateDevices);
      return () => {
        mounted = false;
        navigator.mediaDevices.removeEventListener('devicechange', updateDevices);
      };
    }
    return () => {
      mounted = false;
    };
  }, []);

  const handleSelectMic = (id: string) => {
    setSelectedMicId(id);
    setPreferredDevice('mic', id);
  };

  const handleSelectSpeaker = (id: string) => {
    setSelectedSpeakerId(id);
    setPreferredDevice('speaker', id);
  };

  const handleSelectCam = (id: string) => {
    setSelectedCamId(id);
    setPreferredDevice('cam', id);
  };

  const handleGuestJoin = (e: React.FormEvent) => {
    e.preventDefault();
    const validation = validateJoinInput(guestName, guestRoomSlug);
    if (!validation.valid) {
      setGuestError(validation.error || 'Invalid input');
      return;
    }
    setGuestError('');
    onJoinRoom(guestName.trim(), guestRoomSlug.trim(), 'guest', {
      micOn,
      camOn,
      audioDeviceId: selectedMicId || undefined,
      videoDeviceId: selectedCamId || undefined,
      speakerDeviceId: selectedSpeakerId || undefined,
    });
  };

  const handleHostAuth = async (e: React.FormEvent) => {
    e.preventDefault();
    setHostError('');
    setHostBusy(true);
    try {
      const session =
        authMode === 'signup'
          ? await signup(hostName.trim(), hostEmail.trim(), hostPassword)
          : await login(hostEmail.trim(), hostPassword);
      setHostSession(session);
    } catch (err) {
      setHostError(err instanceof Error ? err.message : 'Authentication failed');
    } finally {
      setHostBusy(false);
    }
  };

  const handleHostCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!hostSession) return;
    setHostError('');
    setHostBusy(true);
    try {
      const roomData = await createRoom(hostRoomSlug.trim(), hostSession.token);
      onEnterAsHost(hostSession, roomData.room.slug, {
        micOn,
        camOn,
        audioDeviceId: selectedMicId || undefined,
        videoDeviceId: selectedCamId || undefined,
        speakerDeviceId: selectedSpeakerId || undefined,
      });
    } catch (err) {
      setHostError(err instanceof Error ? err.message : 'Failed to create room');
      setHostBusy(false);
    }
  };

  const handleHostLogout = async () => {
    await logout();
    setHostSession(null);
    setHostEmail('');
    setHostPassword('');
  };

  return (
    <div className="min-h-screen w-full bg-slate-950 text-slate-100 flex flex-col justify-center items-center p-6 relative overflow-hidden">
      {/* Background Decorative Gradients */}
      <div className="absolute top-1/4 left-1/4 w-96 h-96 bg-indigo-600/10 rounded-full blur-3xl pointer-events-none" />
      <div className="absolute bottom-1/4 right-1/4 w-96 h-96 bg-cyan-600/10 rounded-full blur-3xl pointer-events-none" />
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[600px] bg-purple-600/5 rounded-full blur-3xl pointer-events-none" />

      {/* Brand Header */}
      <div className="text-center max-w-2xl mx-auto mb-12 z-10">
        <div className="inline-flex items-center gap-2 px-3.5 py-1.5 rounded-full bg-indigo-500/10 border border-indigo-500/20 text-indigo-400 text-xs font-medium mb-6 shadow-sm">
          <Sparkles className="w-3.5 h-3.5" />
          <span>Self-Hosted Distributed WebRTC & Egress Platform</span>
        </div>
        <div className="flex items-center justify-center gap-3 mb-4">
          <div className="w-12 h-12 rounded-2xl bg-gradient-to-br from-indigo-500 to-purple-600 flex items-center justify-center shadow-xl shadow-indigo-500/20 font-bold text-2xl text-white">
            N
          </div>
          <h1 className="text-4xl md:text-5xl font-extrabold tracking-tight bg-clip-text text-transparent bg-gradient-to-r from-white via-slate-200 to-indigo-300">
            Naka Meet
          </h1>
        </div>
        <p className="text-slate-400 text-sm md:text-base leading-relaxed">
          Low-latency SFU video conferencing with high-definition multi-track screen sharing and automated FFmpeg Egress recording directly inside Docker.
        </p>
      </div>

      {/* Main Grid Options */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6 max-w-4xl w-full z-10">
        {/* Card 1: Guest Join (No Login Required) */}
        <div className="glass-panel p-8 rounded-3xl border border-slate-800/80 hover:border-cyan-500/30 transition-all duration-300 flex flex-col justify-between shadow-2xl relative group">
          <div className="absolute top-0 right-0 w-32 h-32 bg-cyan-500/5 rounded-full blur-2xl group-hover:bg-cyan-500/10 transition-all pointer-events-none" />
          <div>
            <div className="flex items-center justify-between mb-6">
              <div className="w-10 h-10 rounded-xl bg-cyan-500/10 border border-cyan-500/20 flex items-center justify-center text-cyan-400">
                <Users className="w-5 h-5" />
              </div>
              <span className="px-3 py-1 bg-cyan-500/10 text-cyan-400 border border-cyan-500/20 rounded-full text-[11px] font-semibold tracking-wide uppercase">
                No Login Required
              </span>
            </div>
            <h2 className="text-xl font-bold text-white mb-2">Join an Existing Room</h2>
            <p className="text-slate-400 text-xs leading-relaxed mb-6">
              Enter directly as a guest participant. No account or password needed to join active meetings and collaborate.
            </p>

            {guestError && (
              <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-xl text-red-400 text-xs flex items-center gap-2">
                <AlertCircle className="w-4 h-4 shrink-0" />
                <span>{guestError}</span>
              </div>
            )}

            <form onSubmit={handleGuestJoin} className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-slate-300 mb-1.5">Display Name</label>
                <input
                  type="text"
                  value={guestName}
                  onChange={(e) => setGuestName(e.target.value)}
                  placeholder="e.g. Budi (Developer)"
                  className="w-full bg-slate-900/80 border border-slate-800 rounded-xl px-4 py-3 text-xs text-slate-100 placeholder:text-slate-600 focus:outline-none focus:border-cyan-500 transition-colors"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-slate-300 mb-1.5">Room Slug or ID</label>
                <input
                  type="text"
                  value={guestRoomSlug}
                  onChange={(e) => setGuestRoomSlug(e.target.value)}
                  placeholder="e.g. demo-room"
                  className="w-full bg-slate-900/80 border border-slate-800 rounded-xl px-4 py-3 text-xs text-slate-100 placeholder:text-slate-600 focus:outline-none focus:border-cyan-500 font-mono transition-colors"
                />
              </div>

              {/* Pre-join Audio & Video Settings */}
              <div className="flex items-center gap-2 pt-1">
                <button
                  type="button"
                  onClick={() => setMicOn(!micOn)}
                  className={`flex-1 py-2.5 px-3 rounded-xl border text-xs font-medium flex items-center justify-center gap-2 transition-all ${
                    micOn
                      ? 'bg-slate-900/90 border-slate-700/80 text-slate-200'
                      : 'bg-red-500/10 border-red-500/30 text-red-400'
                  }`}
                >
                  {micOn ? <Mic className="w-3.5 h-3.5 text-emerald-400" /> : <MicOff className="w-3.5 h-3.5" />}
                  <span>{micOn ? 'Mic Ready' : 'Mic Muted'}</span>
                </button>
                <button
                  type="button"
                  onClick={() => setCamOn(!camOn)}
                  className={`flex-1 py-2.5 px-3 rounded-xl border text-xs font-medium flex items-center justify-center gap-2 transition-all ${
                    camOn
                      ? 'bg-slate-900/90 border-slate-700/80 text-slate-200'
                      : 'bg-red-500/10 border-red-500/30 text-red-400'
                  }`}
                >
                  {camOn ? <Video className="w-3.5 h-3.5 text-emerald-400" /> : <VideoOff className="w-3.5 h-3.5" />}
                  <span>{camOn ? 'Cam Ready' : 'Cam Off'}</span>
                </button>
                <button
                  type="button"
                  onClick={() => setShowDeviceSettings(true)}
                  className="py-2.5 px-3 rounded-xl border bg-slate-900/90 border-slate-700/80 text-slate-300 hover:text-white hover:border-cyan-500/50 transition-all flex items-center justify-center"
                  title="Audio & Video Device Settings"
                >
                  <Settings className="w-3.5 h-3.5" />
                </button>
              </div>

              <button
                type="submit"
                className="w-full mt-2 py-3.5 px-6 bg-gradient-to-r from-cyan-600 to-blue-600 hover:from-cyan-500 hover:to-blue-500 text-white rounded-xl font-semibold text-xs transition-all duration-200 shadow-lg shadow-cyan-600/20 flex items-center justify-center gap-2 group-hover:scale-[1.01]"
              >
                <LogIn className="w-4 h-4" />
                <span>Join Room as Guest</span>
                <ArrowRight className="w-4 h-4 ml-1" />
              </button>
            </form>
          </div>
        </div>

        {/* Card 2: Host Create / Login */}
        <div className="glass-panel p-8 rounded-3xl border border-slate-800/80 hover:border-indigo-500/30 transition-all duration-300 flex flex-col justify-between shadow-2xl relative group">
          <div className="absolute top-0 right-0 w-32 h-32 bg-indigo-500/5 rounded-full blur-2xl group-hover:bg-indigo-500/10 transition-all pointer-events-none" />
          <div>
            <div className="flex items-center justify-between mb-6">
              <div className="w-10 h-10 rounded-xl bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center text-indigo-400">
                <ShieldCheck className="w-5 h-5" />
              </div>
              <span className="px-3 py-1 bg-indigo-500/10 text-indigo-400 border border-indigo-500/20 rounded-full text-[11px] font-semibold tracking-wide uppercase">
                Host Authority
              </span>
            </div>
            <h2 className="text-xl font-bold text-white mb-2">
              {hostSession ? `Welcome, ${hostSession.name}` : 'Host Sign In'}
            </h2>
            <p className="text-slate-400 text-xs leading-relaxed mb-6">
              {hostSession
                ? 'Create or rejoin a room with full authority over Egress recording and RTMP streaming.'
                : 'A real account is required to host — hosting authority is tied to who you are, not a self-declared role.'}
            </p>

            {hostError && (
              <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-xl text-red-400 text-xs flex items-center gap-2">
                <AlertCircle className="w-4 h-4 shrink-0" />
                <span>{hostError}</span>
              </div>
            )}

            {hostSession ? (
              <form onSubmit={handleHostCreate} className="space-y-4">
                <div>
                  <label className="block text-xs font-medium text-slate-300 mb-1.5">
                    Custom Room Slug <span className="text-slate-500 font-normal">(Optional, leave blank to generate)</span>
                  </label>
                  <input
                    type="text"
                    value={hostRoomSlug}
                    onChange={(e) => setHostRoomSlug(e.target.value)}
                    placeholder="e.g. masterclass-golang"
                    className="w-full bg-slate-900/80 border border-slate-800 rounded-xl px-4 py-3 text-xs text-slate-100 placeholder:text-slate-600 focus:outline-none focus:border-indigo-500 font-mono transition-colors"
                  />
                </div>

                {/* Pre-join Audio & Video Settings for Host */}
                <div className="flex items-center gap-2 pt-1">
                  <button
                    type="button"
                    onClick={() => setMicOn(!micOn)}
                    className={`flex-1 py-2.5 px-3 rounded-xl border text-xs font-medium flex items-center justify-center gap-2 transition-all ${
                      micOn
                        ? 'bg-slate-900/90 border-slate-700/80 text-slate-200'
                        : 'bg-red-500/10 border-red-500/30 text-red-400'
                    }`}
                  >
                    {micOn ? <Mic className="w-3.5 h-3.5 text-emerald-400" /> : <MicOff className="w-3.5 h-3.5" />}
                    <span>{micOn ? 'Mic Ready' : 'Mic Muted'}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setCamOn(!camOn)}
                    className={`flex-1 py-2.5 px-3 rounded-xl border text-xs font-medium flex items-center justify-center gap-2 transition-all ${
                      camOn
                        ? 'bg-slate-900/90 border-slate-700/80 text-slate-200'
                        : 'bg-red-500/10 border-red-500/30 text-red-400'
                    }`}
                  >
                    {camOn ? <Video className="w-3.5 h-3.5 text-emerald-400" /> : <VideoOff className="w-3.5 h-3.5" />}
                    <span>{camOn ? 'Cam Ready' : 'Cam Off'}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setShowDeviceSettings(true)}
                    className="py-2.5 px-3 rounded-xl border bg-slate-900/90 border-slate-700/80 text-slate-300 hover:text-white hover:border-indigo-500/50 transition-all flex items-center justify-center"
                    title="Audio & Video Device Settings"
                  >
                    <Settings className="w-3.5 h-3.5" />
                  </button>
                </div>

                <button
                  type="submit"
                  disabled={hostBusy}
                  className="w-full mt-2 py-3.5 px-6 bg-gradient-to-r from-indigo-600 to-purple-600 hover:from-indigo-500 hover:to-purple-500 text-white rounded-xl font-semibold text-xs transition-all duration-200 shadow-lg shadow-indigo-600/20 flex items-center justify-center gap-2 group-hover:scale-[1.01] disabled:opacity-60"
                >
                  <PlusCircle className="w-4 h-4" />
                  <span>{hostBusy ? 'Creating…' : 'Create & Join as Host'}</span>
                  <ArrowRight className="w-4 h-4 ml-1" />
                </button>
                <button
                  type="button"
                  onClick={handleHostLogout}
                  className="w-full py-2 text-[11px] text-slate-500 hover:text-slate-300 flex items-center justify-center gap-1.5"
                >
                  <LogOut className="w-3.5 h-3.5" />
                  Not you? Log out
                </button>
              </form>
            ) : (
              <form onSubmit={handleHostAuth} className="space-y-4">
                {authMode === 'signup' && (
                  <div>
                    <label className="block text-xs font-medium text-slate-300 mb-1.5">Host Name</label>
                    <input
                      type="text"
                      value={hostName}
                      onChange={(e) => setHostName(e.target.value)}
                      placeholder="e.g. Azhar (Instructor)"
                      className="w-full bg-slate-900/80 border border-slate-800 rounded-xl px-4 py-3 text-xs text-slate-100 placeholder:text-slate-600 focus:outline-none focus:border-indigo-500 transition-colors"
                    />
                  </div>
                )}
                <div>
                  <label className="block text-xs font-medium text-slate-300 mb-1.5">Email</label>
                  <input
                    type="email"
                    value={hostEmail}
                    onChange={(e) => setHostEmail(e.target.value)}
                    placeholder="you@example.com"
                    className="w-full bg-slate-900/80 border border-slate-800 rounded-xl px-4 py-3 text-xs text-slate-100 placeholder:text-slate-600 focus:outline-none focus:border-indigo-500 transition-colors"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-slate-300 mb-1.5">Password</label>
                  <input
                    type="password"
                    value={hostPassword}
                    onChange={(e) => setHostPassword(e.target.value)}
                    placeholder={authMode === 'signup' ? 'At least 8 characters' : '••••••••'}
                    className="w-full bg-slate-900/80 border border-slate-800 rounded-xl px-4 py-3 text-xs text-slate-100 placeholder:text-slate-600 focus:outline-none focus:border-indigo-500 transition-colors"
                  />
                </div>
                <button
                  type="submit"
                  disabled={hostBusy}
                  className="w-full mt-2 py-3.5 px-6 bg-gradient-to-r from-indigo-600 to-purple-600 hover:from-indigo-500 hover:to-purple-500 text-white rounded-xl font-semibold text-xs transition-all duration-200 shadow-lg shadow-indigo-600/20 flex items-center justify-center gap-2 group-hover:scale-[1.01] disabled:opacity-60"
                >
                  {authMode === 'signup' ? <UserPlus className="w-4 h-4" /> : <LogIn className="w-4 h-4" />}
                  <span>
                    {hostBusy ? 'Please wait…' : authMode === 'signup' ? 'Sign Up' : 'Log In'}
                  </span>
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setAuthMode(authMode === 'signup' ? 'login' : 'signup');
                    setHostError('');
                  }}
                  className="w-full py-1 text-[11px] text-slate-400 hover:text-slate-200"
                >
                  {authMode === 'signup' ? 'Already have an account? Log in' : "Don't have an account? Sign up"}
                </button>
              </form>
            )}
          </div>
        </div>
      </div>

      {/* Footer Info */}
      <div className="mt-12 text-center text-slate-500 text-xs flex items-center gap-6 z-10">
        <span className="flex items-center gap-1.5">
          <span className="w-2 h-2 rounded-full bg-emerald-500 inline-block animate-pulse" /> SFU Latency &lt; 200ms
        </span>
        <span>•</span>
        <span>Hard Limit: 50 Participants/Room</span>
        <span>•</span>
        <span>Powered by Pion Go WebRTC &amp; Puppeteer Egress</span>
      </div>

      {/* Audio & Video Device Settings Modal */}
      {showDeviceSettings && (
        <div className="fixed inset-0 bg-slate-950/80 backdrop-blur-md z-50 flex items-center justify-center p-4">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 w-full max-w-md shadow-2xl relative">
            <button
              onClick={() => setShowDeviceSettings(false)}
              className="absolute top-4 right-4 text-slate-400 hover:text-slate-200"
              title="Close Settings"
            >
              <X className="w-5 h-5" />
            </button>

            <div className="flex items-center gap-3 mb-5">
              <div className="p-2.5 bg-indigo-500/10 border border-indigo-500/20 rounded-xl text-indigo-400">
                <Settings className="w-6 h-6" />
              </div>
              <div>
                <h3 className="font-semibold text-slate-100 text-base">Audio & Video Settings</h3>
                <p className="text-xs text-slate-400">Pilih mikrofon, speaker, dan kamera</p>
              </div>
            </div>

            <div className="space-y-4">
              {/* Microphone Select */}
              <div>
                <label className="block text-xs font-medium text-slate-300 mb-1.5 flex items-center gap-1.5">
                  <Mic className="w-3.5 h-3.5 text-indigo-400" />
                  Microphone
                </label>
                <select
                  aria-label="Select Microphone"
                  value={selectedMicId}
                  onChange={(e) => handleSelectMic(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-indigo-500"
                >
                  <option value="">Default Microphone</option>
                  {devices.microphones.map((d, i) => (
                    <option key={d.deviceId || i} value={d.deviceId}>
                      {d.label || `Microphone ${i + 1}`}
                    </option>
                  ))}
                </select>
              </div>

              {/* Speaker Select */}
              <div>
                <label className="block text-xs font-medium text-slate-300 mb-1.5 flex items-center gap-1.5">
                  <Volume2 className="w-3.5 h-3.5 text-indigo-400" />
                  Speaker / Output
                </label>
                <div className="flex gap-2">
                  <select
                    aria-label="Select Speaker"
                    value={selectedSpeakerId}
                    onChange={(e) => handleSelectSpeaker(e.target.value)}
                    className="flex-1 bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-indigo-500"
                  >
                    <option value="">Default Speaker</option>
                    {devices.speakers.map((d, i) => (
                      <option key={d.deviceId || i} value={d.deviceId}>
                        {d.label || `Speaker ${i + 1}`}
                      </option>
                    ))}
                  </select>
                  <button
                    type="button"
                    disabled={isPlayingTest}
                    onClick={async () => {
                      setIsPlayingTest(true);
                      try {
                        await playTestSpeakerTone(selectedSpeakerId);
                      } finally {
                        setIsPlayingTest(false);
                      }
                    }}
                    className="px-3 py-2 bg-slate-800 hover:bg-slate-700 disabled:opacity-50 text-indigo-400 text-xs font-medium rounded-xl border border-slate-700/50 flex items-center gap-1.5 whitespace-nowrap"
                  >
                    <Volume2 className="w-3.5 h-3.5" />
                    {isPlayingTest ? 'Playing…' : 'Test'}
                  </button>
                </div>
                {!isSinkIdSupported() && (
                  <p className="text-[11px] text-amber-400/80 mt-1">
                    Browser ini tidak mendukung pemilihan speaker kustom (setSinkId). Menggunakan output default sistem.
                  </p>
                )}
              </div>

              {/* Camera Select */}
              <div>
                <label className="block text-xs font-medium text-slate-300 mb-1.5 flex items-center gap-1.5">
                  <Video className="w-3.5 h-3.5 text-indigo-400" />
                  Camera
                </label>
                <select
                  aria-label="Select Camera"
                  value={selectedCamId}
                  onChange={(e) => handleSelectCam(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-100 focus:outline-none focus:border-indigo-500"
                >
                  <option value="">Default Camera</option>
                  {devices.cameras.map((d, i) => (
                    <option key={d.deviceId || i} value={d.deviceId}>
                      {d.label || `Camera ${i + 1}`}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div className="flex items-center justify-end gap-3 pt-5 mt-4 border-t border-slate-800">
              <button
                type="button"
                onClick={() => setShowDeviceSettings(false)}
                className="px-4 py-2 text-xs font-medium text-white bg-indigo-600 hover:bg-indigo-500 rounded-xl shadow-lg shadow-indigo-600/30"
              >
                Selesai
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
