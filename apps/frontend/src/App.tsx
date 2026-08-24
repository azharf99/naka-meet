import React, { useEffect, useState } from 'react';
import { WebRTCService, ParticipantTrack } from './services/webrtc';
import { guestJoin, getSession, logout } from './services/auth';
import { VideoGrid } from './components/VideoGrid';
import { Controls } from './components/Controls';
import { Lobby, HostSession } from './components/Lobby';
import { Send, X, ShieldCheck, UserCheck, LogOut, UserX } from 'lucide-react';

export const App: React.FC = () => {
  const urlParams = new URLSearchParams(window.location.search);
  const initialRoomFromUrl = urlParams.get('room') || '';

  const [inMeeting, setInMeeting] = useState<boolean>(false);
  const [roomSlug, setRoomSlug] = useState<string>(initialRoomFromUrl);
  const [displayName, setDisplayName] = useState<string>('');
  const [token, setToken] = useState<string>('');
  const [userRole, setUserRole] = useState<string>('host');
  // This client's own raw participant ID — distinct from displayName,
  // needed to tell whether the room's active presentation (identified by
  // peer ID, see activePresentation below) is this client's own screen
  // share or someone else's.
  const [localPeerId, setLocalPeerId] = useState<string>('');

  const [webrtcService, setWebrtcService] = useState<WebRTCService | null>(null);
  const [localStream, setLocalStream] = useState<MediaStream | null>(null);
  const [localScreenStream, setLocalScreenStream] = useState<MediaStream | null>(null);
  const [remoteTracks, setRemoteTracks] = useState<ParticipantTrack[]>([]);
  const [remoteMediaState, setRemoteMediaState] = useState<Map<string, { mic: boolean; cam: boolean }>>(new Map());
  // Keyed the same way as remoteMediaState (by peerID/display name) — true
  // while the server's RTP-liveness watchdog has flagged that publisher's
  // track as silent (frozen video that isn't recovering on its own).
  const [remoteStaleState, setRemoteStaleState] = useState<Map<string, boolean>>(new Map());
  const [connectionLost, setConnectionLost] = useState(false);
  const [connectionRejected, setConnectionRejected] = useState(false);
  // Set when THIS client is force-removed (by the host, or by the
  // stale-timeout auto-removal) — a takeover notice rather than a small
  // banner, since unlike a network drop there's nothing to reload/retry.
  const [removedReason, setRemovedReason] = useState<string | null>(null);
  const [recordingConsent, setRecordingConsent] = useState<{ active: boolean; kind: string } | null>(null);
  // The room's currently-arbitrated active screen share (BR: latest share
  // wins by default; host can pick or reclaim). null when nobody is
  // presenting — distinct from "someone is presenting but suspended,"
  // which VideoGrid derives locally from remoteTracks + this peer ID.
  const [activePresentation, setActivePresentation] = useState<{ peerId: string; peerName: string } | null>(null);

  const [chatOpen, setChatOpen] = useState(false);
  const [messages, setMessages] = useState<Array<{ sender: string; text: string; time: string }>>([]);
  const [inputText, setInputText] = useState('');
  const [isRecording, setIsRecording] = useState(false);
  const [isLiveStreaming, setIsLiveStreaming] = useState(false);
  const [isScreenSharing, setIsScreenSharing] = useState(false);

  const [restoredHostSession, setRestoredHostSession] = useState<HostSession | null>(null);
  const [sessionChecked, setSessionChecked] = useState(false);

  const handleJoinRoom = async (name: string, targetRoomSlug: string, role: string) => {
    try {
      const authData = await guestJoin(name, targetRoomSlug, role);
      setToken(authData.token);
      setUserRole(authData.role || role);
      setLocalPeerId(authData.user_id);
      setDisplayName(name);
      setRoomSlug(targetRoomSlug);
      setInMeeting(true);
      window.history.pushState({}, '', `?room=${encodeURIComponent(targetRoomSlug)}`);
    } catch (err) {
      console.error('Failed to join room', err);
      alert('Failed to join room. Please check backend connection.');
    }
  };

  // Called by Lobby once a host has authenticated (login/signup, or a
  // restored cookie session) AND the room has already been created — auth
  // and room-ownership are Lobby's concern, App just enters the meeting.
  const handleEnterAsHost = (session: HostSession, activeSlug: string) => {
    setToken(session.token);
    setUserRole(session.role || 'host');
    setLocalPeerId(session.user_id);
    setDisplayName(session.name);
    setRoomSlug(activeSlug);
    setInMeeting(true);
    window.history.pushState({}, '', `?room=${encodeURIComponent(activeSlug)}`);
  };

  // Restores a host session from the httpOnly cookie so a returning host
  // doesn't have to re-enter credentials after a reload.
  useEffect(() => {
    const roleParam = urlParams.get('role');
    if (roleParam === 'egress') {
      setSessionChecked(true);
      return;
    }
    getSession().then((session) => {
      if (session && session.role === 'host') {
        // /auth/me deliberately doesn't return the raw JWT (it only proves
        // the httpOnly cookie is valid) — leave token empty. Every request
        // that follows uses credentials:'include', and the backend already
        // prefers the cookie over an empty/missing Bearer header, so this
        // still authenticates correctly for both REST and the WS handshake.
        setRestoredHostSession({ token: '', name: session.name, role: session.role, user_id: session.user_id });
      }
      setSessionChecked(true);
    });
    // urlParams is rebuilt fresh from window.location.search on every render
    // (not memoized) and this effect must run exactly once on mount to
    // resolve the initial "is there a restorable host session" state —
    // adding it to the deps array would make a new URLSearchParams object
    // identity re-trigger the effect on every render instead.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const roleParam = urlParams.get('role');
    const roomParam = urlParams.get('room');
    if (roleParam === 'egress' && roomParam && !inMeeting) {
      handleJoinRoom('Egress Recorder', roomParam, 'egress');
    }
    // Same rationale as above for urlParams; inMeeting is read here only to
    // guard against re-joining if this ever re-ran, not to react to it —
    // including it would turn "auto-join once as the egress bot" into
    // "auto-join again every time the meeting is left."
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);


  const handleLeaveRoom = () => {
    webrtcService?.disconnect();
    setWebrtcService(null);
    setLocalStream(null);
    setLocalScreenStream(null);
    setIsScreenSharing(false);
    setRemoteTracks([]);
    setMessages([]);
    setConnectionLost(false);
    setConnectionRejected(false);
    setRemoteStaleState(new Map());
    setRemovedReason(null);
    setActivePresentation(null);
    setInMeeting(false);
    window.history.pushState({}, '', window.location.pathname);
  };

  useEffect(() => {
    if (!inMeeting || !token || !roomSlug) return;

    let activeService: WebRTCService | null = null;

    async function connectWebRTC() {
      try {
        const service = new WebRTCService(roomSlug);
        activeService = service;

        service.onTrackAdded = (track) => {
          setRemoteTracks((prev) => [...prev.filter((t) => t.id !== track.id), track]);
        };

        service.onTrackRemoved = (idOrPeerId) => {
          setRemoteTracks((prev) =>
            prev.filter(
              (t) =>
                t.id !== idOrPeerId &&
                t.peerID !== idOrPeerId &&
                t.stream?.id !== idOrPeerId
            )
          );
          // Prevents a stale mute/cam-off badge from surviving a participant
          // leaving and a later participant reusing the same display name.
          setRemoteMediaState((prev) => {
            if (!prev.has(idOrPeerId)) return prev;
            const next = new Map(prev);
            next.delete(idOrPeerId);
            return next;
          });
        };

        service.onScreenShareEnded = () => {
          setLocalScreenStream(null);
          setIsScreenSharing(false);
        };

        service.onMessageReceived = (msg) => {
          setMessages((prev) => [...prev, msg]);
        };

        service.onMediaStateChanged = ({ peerId, kind, enabled }) => {
          setRemoteMediaState((prev) => {
            const next = new Map(prev);
            const cur = next.get(peerId) || { mic: true, cam: true };
            next.set(peerId, { ...cur, [kind]: enabled });
            return next;
          });
        };

        service.onDisconnected = (wasConnected) => {
          setConnectionLost(true);
          // The WS never reached `open` at all — almost always this origin
          // isn't in the server's ALLOWED_ORIGINS allowlist (e.g. joining
          // via a LAN IP or 127.0.0.1 that isn't listed), not a mid-call
          // network drop. Worth a distinct message: "reload" doesn't fix it.
          setConnectionRejected(!wasConnected);
        };

        service.onPeerStaleChanged = ({ peerId, stale }) => {
          setRemoteStaleState((prev) => {
            const next = new Map(prev);
            if (stale) {
              next.set(peerId, true);
            } else {
              next.delete(peerId);
            }
            return next;
          });
        };

        // Distinct from onDisconnected: this client was force-removed, not
        // dropped — nothing to reload/retry, so it gets its own takeover
        // notice instead of the generic connection-lost banner.
        service.onRemoved = (reason) => {
          setRemovedReason(reason);
        };

        service.onPresentationStateChanged = ({ activePeerId, activePeerName }) => {
          setActivePresentation(activePeerId ? { peerId: activePeerId, peerName: activePeerName } : null);
        };

        // Server-triggered broadcast (from the host's REST call, not this
        // peer's own click) — every participant, host included, needs the
        // on-screen recording-consent notice, not just whoever clicked.
        service.onRecordingStateChanged = ({ active, kind }) => {
          setRecordingConsent(active ? { active, kind } : null);
        };

        await service.connectToken(token);
        setConnectionLost(false);
        setLocalStream(service.getLocalStream());
        setWebrtcService(service);
      } catch (err) {
        console.error('WebRTC connection error', err);
      }
    }

    connectWebRTC();

    return () => {
      activeService?.disconnect();
    };
  }, [inMeeting, token, roomSlug]);

  const handleSendMessage = (e: React.FormEvent) => {
    e.preventDefault();
    if (!inputText.trim() || !webrtcService) return;
    webrtcService.sendMessage(inputText);
    setInputText('');
  };

  const sendEgressCommand = async (action: string, url?: string) => {
    try {
      const res = await fetch(`/api/v1/rooms/${roomSlug}/live`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        credentials: 'include',
        body: JSON.stringify({ action, url }),
      });
      if (res.ok) {
        if (action === 'START_RECORDING') {
          setIsRecording(true);
          setIsLiveStreaming(false);
        } else if (action === 'START_RTMP') {
          setIsLiveStreaming(true);
          setIsRecording(false);
        } else if (action === 'STOP_EGRESS') {
          setIsRecording(false);
          setIsLiveStreaming(false);
        }
      } else {
        console.error('Egress command failed', await res.text());
      }
    } catch (err) {
      console.error('Failed to send egress command', err);
    }
  };

  const handleStartRecording = () => sendEgressCommand('START_RECORDING');
  const handleStartRTMP = (url: string) => sendEgressCommand('START_RTMP', url);
  const handleStopEgress = () => sendEgressCommand('STOP_EGRESS');

  // Host-only moderation action (BR1). The target actually disconnects via
  // the signaling WebSocket server-side (see signaling.Handler.RemoveParticipant)
  // — this call just authorizes and triggers it; the removed participant's
  // own client shows the takeover notice via WebRTCService.onRemoved once
  // their connection receives the "removed" message.
  const handleRemoveParticipant = async (rawPeerId: string) => {
    if (!rawPeerId) return;
    try {
      const res = await fetch(
        `/api/v1/rooms/${roomSlug}/participants/${encodeURIComponent(rawPeerId)}/remove`,
        {
          method: 'POST',
          headers: { Authorization: `Bearer ${token}` },
          credentials: 'include',
        }
      );
      if (!res.ok) {
        console.error('Failed to remove participant', await res.text());
      }
    } catch (err) {
      console.error('Failed to remove participant', err);
    }
  };

  // Host-only screen-share arbitration override — see WebRTCService.setPresentation.
  const handleSetPresentation = (peerId: string) => {
    webrtcService?.setPresentation(peerId);
  };

  if (!inMeeting) {
    if (!sessionChecked) {
      return <div className="min-h-screen w-full bg-slate-950" />;
    }
    return (
      <Lobby
        initialRoomSlug={initialRoomFromUrl}
        initialHostSession={restoredHostSession}
        onJoinRoom={handleJoinRoom}
        onEnterAsHost={handleEnterAsHost}
      />
    );
  }

  return (
    <div className="flex h-screen w-screen bg-slate-950 text-slate-100 overflow-hidden relative">
      {/* Top Header */}
      <header className="absolute top-0 left-0 right-0 h-16 glass-panel border-b border-slate-800/80 px-6 flex items-center justify-between z-40">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-indigo-600 flex items-center justify-center font-bold text-white shadow-lg shadow-indigo-500/20">
            N
          </div>
          <div>
            <h1 className="font-semibold text-sm tracking-wide text-slate-100">Naka Meet</h1>
            <p className="text-xs text-slate-400 flex items-center gap-1">
              Room: <span className="text-indigo-400 font-mono">{roomSlug}</span>
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          {displayName && (
            <span className="px-3 py-1 bg-slate-800/80 text-slate-300 border border-slate-700/60 rounded-full text-xs font-medium">
              {displayName}
            </span>
          )}
          <span className="px-3 py-1 bg-indigo-500/10 text-indigo-400 border border-indigo-500/20 rounded-full text-xs font-medium flex items-center gap-1.5 capitalize">
            <UserCheck className="w-3.5 h-3.5" /> Role: {userRole}
          </span>
          <span className="px-3 py-1 bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 rounded-full text-xs font-medium flex items-center gap-1.5">
            <ShieldCheck className="w-3.5 h-3.5" /> WebRTC SFU Active
          </span>
          {userRole === 'host' && (
            <button
              onClick={() => {
                logout();
                setRestoredHostSession(null);
                handleLeaveRoom();
              }}
              className="p-2 bg-slate-800/80 hover:bg-slate-700 text-slate-300 border border-slate-700/60 rounded-full transition-colors"
              title="Log out"
            >
              <LogOut className="w-3.5 h-3.5" />
            </button>
          )}
        </div>
      </header>

      {/* Main Video Area */}
      <main className="flex-1 flex pt-16 relative">
        <VideoGrid
          localStream={localStream}
          localScreenStream={localScreenStream}
          remoteTracks={remoteTracks}
          displayName={displayName}
          userRole={userRole}
          remoteMediaState={remoteMediaState}
          remoteStaleState={remoteStaleState}
          onRemoveParticipant={userRole === 'host' ? handleRemoveParticipant : undefined}
          localPeerId={localPeerId}
          activePresentation={activePresentation}
          onSetPresentation={userRole === 'host' ? handleSetPresentation : undefined}
        />

        {/* Removal takeover notice: unlike a dropped connection, there's
            nothing to reload/retry here, so this replaces the meeting view
            entirely instead of sitting alongside it as a small banner. */}
        {removedReason && (
          <div className="absolute inset-0 z-[60] bg-slate-950/90 backdrop-blur-md flex items-center justify-center p-4">
            <div className="bg-slate-900 border border-slate-800 rounded-2xl p-8 max-w-sm w-full text-center shadow-2xl">
              <div className="w-14 h-14 mx-auto mb-4 rounded-full bg-red-500/10 border border-red-500/20 flex items-center justify-center text-red-400">
                <UserX className="w-7 h-7" />
              </div>
              <h3 className="font-semibold text-slate-100 text-base mb-2">You were removed from this meeting</h3>
              <p className="text-xs text-slate-400 mb-6">
                {removedReason === 'stale_timeout'
                  ? 'Your video appeared to freeze and did not recover in time, so you were disconnected.'
                  : 'The host removed you from this room.'}
              </p>
              <button
                onClick={handleLeaveRoom}
                className="px-4 py-2 text-xs font-medium text-white bg-indigo-600 hover:bg-indigo-500 rounded-xl shadow-lg shadow-indigo-600/30"
              >
                Return to Lobby
              </button>
            </div>
          </div>
        )}

        <div className="absolute top-4 left-1/2 -translate-x-1/2 z-50 flex flex-col items-center gap-2">
          {connectionLost && (
            <div className="px-4 py-2 bg-red-500/90 text-white text-xs font-medium rounded-lg shadow-lg backdrop-blur-md">
              {connectionRejected
                ? "Couldn't connect — this address isn't allowed by the server. Ask the host to add it to ALLOWED_ORIGINS."
                : 'Connection lost — please reload to rejoin.'}
            </div>
          )}

          {recordingConsent?.active && (
            <div className="px-4 py-2 bg-red-600/90 text-white text-xs font-semibold rounded-lg shadow-lg backdrop-blur-md flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-white animate-pulse" />
              This meeting is being {recordingConsent.kind === 'rtmp' ? 'live streamed' : 'recorded'}.
            </div>
          )}
        </div>

        {/* Real-time Chat Drawer (relayed by the SFU over the signaling WebSocket) */}
        {chatOpen && (
          <aside className="w-80 h-[calc(100vh-64px)] glass-panel border-l border-slate-800 flex flex-col z-40 transition-all duration-300">
            <div className="p-4 border-b border-slate-800 flex items-center justify-between">
              <h3 className="font-semibold text-sm text-slate-200">Chat</h3>
              <button onClick={() => setChatOpen(false)} className="text-slate-400 hover:text-slate-200">
                <X className="w-4 h-4" />
              </button>
            </div>

            <div className="flex-1 p-4 overflow-y-auto space-y-3">
              {messages.map((m, idx) => (
                <div key={idx} className={`flex flex-col ${m.sender === 'You' ? 'items-end' : 'items-start'}`}>
                  <div className="flex items-center gap-2 mb-1 text-[10px] text-slate-400">
                    <span>{m.sender}</span>
                    <span>•</span>
                    <span>{m.time}</span>
                  </div>
                  <div
                    className={`px-3 py-2 rounded-xl text-xs max-w-[85%] ${
                      m.sender === 'You'
                        ? 'bg-indigo-600 text-white rounded-br-none'
                        : 'bg-slate-800 text-slate-200 rounded-bl-none'
                    }`}
                  >
                    {m.text}
                  </div>
                </div>
              ))}
            </div>

            <form onSubmit={handleSendMessage} className="p-3 border-t border-slate-800 flex gap-2">
              <input
                type="text"
                value={inputText}
                onChange={(e) => setInputText(e.target.value)}
                placeholder="Type a message..."
                className="flex-1 bg-slate-900 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-200 focus:outline-none focus:border-indigo-500"
              />
              <button
                type="submit"
                className="p-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-xl transition-all duration-200"
              >
                <Send className="w-4 h-4" />
              </button>
            </form>
          </aside>
        )}
      </main>

      {/* Control Bar */}
      <Controls
        userRole={userRole}
        isRecording={isRecording}
        isLiveStreaming={isLiveStreaming}
        isScreenSharing={isScreenSharing}
        onToggleMic={() => {
          if (localStream) {
            localStream.getAudioTracks().forEach((t) => (t.enabled = !t.enabled));
            const nowEnabled = localStream.getAudioTracks()[0]?.enabled ?? true;
            webrtcService?.sendMediaState('mic', nowEnabled);
          }
        }}
        onToggleCam={() => {
          if (localStream) {
            localStream.getVideoTracks().forEach((t) => (t.enabled = !t.enabled));
            const nowEnabled = localStream.getVideoTracks()[0]?.enabled ?? true;
            webrtcService?.sendMediaState('cam', nowEnabled);
          }
        }}
        onScreenShare={async () => {
          const screenStream = await webrtcService?.startScreenShare();
          if (screenStream) {
            setLocalScreenStream(screenStream);
            setIsScreenSharing(true);
          }
        }}
        onStopScreenShare={() => {
          webrtcService?.stopScreenShare();
          setLocalScreenStream(null);
          setIsScreenSharing(false);
        }}
        onToggleChat={() => setChatOpen(!chatOpen)}
        onStartRecording={handleStartRecording}
        onStartRTMP={handleStartRTMP}
        onStopEgress={handleStopEgress}
        onLeave={handleLeaveRoom}
      />



    </div>
  );
};

export default App;

