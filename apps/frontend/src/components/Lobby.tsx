import React, { useState } from 'react';
import { Users, PlusCircle, Sparkles, LogIn, ShieldCheck, Video, ArrowRight, AlertCircle } from 'lucide-react';

export function validateJoinInput(name: string, roomSlug: string): { valid: boolean; error?: string } {
  if (!name.trim()) return { valid: false, error: 'Display name is required' };
  if (!roomSlug.trim()) return { valid: false, error: 'Room name is required' };
  return { valid: true };
}

interface LobbyProps {
  initialRoomSlug?: string;
  onJoinRoom: (name: string, roomSlug: string, role: string) => void;
  onCreateRoom: (name: string, roomSlug: string) => void;
}


export const Lobby: React.FC<LobbyProps> = ({ initialRoomSlug = '', onJoinRoom, onCreateRoom }) => {
  const [guestName, setGuestName] = useState('');
  const [guestRoomSlug, setGuestRoomSlug] = useState(initialRoomSlug || 'demo-room');
  const [guestError, setGuestError] = useState('');

  const [hostName, setHostName] = useState('');
  const [hostRoomSlug, setHostRoomSlug] = useState('');
  const [hostError, setHostError] = useState('');

  const handleGuestJoin = (e: React.FormEvent) => {
    e.preventDefault();
    const validation = validateJoinInput(guestName, guestRoomSlug);
    if (!validation.valid) {
      setGuestError(validation.error || 'Invalid input');
      return;
    }
    setGuestError('');
    onJoinRoom(guestName.trim(), guestRoomSlug.trim(), 'guest');
  };

  const handleHostCreate = (e: React.FormEvent) => {
    e.preventDefault();
    if (!hostName.trim()) {
      setHostError('Host display name is required');
      return;
    }
    setHostError('');
    onCreateRoom(hostName.trim(), hostRoomSlug.trim());
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
            <h2 className="text-xl font-bold text-white mb-2">Create or Manage Room</h2>
            <p className="text-slate-400 text-xs leading-relaxed mb-6">
              Enter as Host to establish new rooms with full authority over Egress recording, RTMP streaming, and room moderation.
            </p>

            {hostError && (
              <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-xl text-red-400 text-xs flex items-center gap-2">
                <AlertCircle className="w-4 h-4 shrink-0" />
                <span>{hostError}</span>
              </div>
            )}

            <form onSubmit={handleHostCreate} className="space-y-4">
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
              <button
                type="submit"
                className="w-full mt-2 py-3.5 px-6 bg-gradient-to-r from-indigo-600 to-purple-600 hover:from-indigo-500 hover:to-purple-500 text-white rounded-xl font-semibold text-xs transition-all duration-200 shadow-lg shadow-indigo-600/20 flex items-center justify-center gap-2 group-hover:scale-[1.01]"
              >
                <PlusCircle className="w-4 h-4" />
                <span>Create & Join as Host</span>
                <ArrowRight className="w-4 h-4 ml-1" />
              </button>
            </form>
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
    </div>
  );
};
