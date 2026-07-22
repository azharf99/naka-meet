import React, { useEffect, useState } from 'react';
import { WebRTCService, ParticipantTrack } from './services/webrtc';
import { loginUser, createRoom } from './services/auth';
import { VideoGrid } from './components/VideoGrid';
import { Controls } from './components/Controls';
import { Lobby } from './components/Lobby';
import { Send, X, ShieldCheck, UserCheck } from 'lucide-react';

export const App: React.FC = () => {
  const urlParams = new URLSearchParams(window.location.search);
  const initialRoomFromUrl = urlParams.get('room') || '';

  const [inMeeting, setInMeeting] = useState<boolean>(false);
  const [roomSlug, setRoomSlug] = useState<string>(initialRoomFromUrl);
  const [displayName, setDisplayName] = useState<string>('');
  const [token, setToken] = useState<string>('');
  const [userRole, setUserRole] = useState<string>('host');

  const [webrtcService, setWebrtcService] = useState<WebRTCService | null>(null);
  const [localStream, setLocalStream] = useState<MediaStream | null>(null);
  const [localScreenStream, setLocalScreenStream] = useState<MediaStream | null>(null);
  const [remoteTracks, setRemoteTracks] = useState<ParticipantTrack[]>([]);

  const [chatOpen, setChatOpen] = useState(false);
  const [messages, setMessages] = useState<Array<{ sender: string; text: string; time: string }>>([]);
  const [inputText, setInputText] = useState('');
  const [isRecording, setIsRecording] = useState(false);
  const [isLiveStreaming, setIsLiveStreaming] = useState(false);
  const [isScreenSharing, setIsScreenSharing] = useState(false);



  const handleJoinRoom = async (name: string, targetRoomSlug: string, role: string) => {
    try {
      const authData = await loginUser(name, role);
      setToken(authData.token);
      setUserRole(authData.role || role);
      setDisplayName(name);
      setRoomSlug(targetRoomSlug);
      setInMeeting(true);
      window.history.pushState({}, '', `?room=${encodeURIComponent(targetRoomSlug)}`);
    } catch (err) {
      console.error('Failed to join room', err);
      alert('Failed to join room. Please check backend connection.');
    }
  };

  const handleCreateRoom = async (name: string, customRoomSlug: string) => {
    try {
      const authData = await loginUser(name, 'host');
      const roomData = await createRoom(customRoomSlug, authData.token);
      const activeSlug = roomData.room.slug;

      setToken(authData.token);
      setUserRole('host');
      setDisplayName(name);
      setRoomSlug(activeSlug);
      setInMeeting(true);
      window.history.pushState({}, '', `?room=${encodeURIComponent(activeSlug)}`);
    } catch (err) {
      console.error('Failed to create room', err);
      alert('Failed to create room. Please check backend connection.');
    }
  };

  useEffect(() => {
    const roleParam = urlParams.get('role');
    const roomParam = urlParams.get('room');
    if (roleParam === 'egress' && roomParam && !inMeeting) {
      handleJoinRoom('Egress Recorder', roomParam, 'egress');
    }
  }, []);


  const handleLeaveRoom = () => {
    webrtcService?.disconnect();
    setWebrtcService(null);
    setLocalStream(null);
    setLocalScreenStream(null);
    setIsScreenSharing(false);
    setRemoteTracks([]);
    setMessages([]);
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
        };

        service.onScreenShareEnded = () => {
          setLocalScreenStream(null);
          setIsScreenSharing(false);
        };

        service.onMessageReceived = (msg) => {
          setMessages((prev) => [...prev, msg]);
        };

        await service.connectToken(token);
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


  if (!inMeeting) {
    return (
      <Lobby
        initialRoomSlug={initialRoomFromUrl}
        onJoinRoom={handleJoinRoom}
        onCreateRoom={handleCreateRoom}
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
        />

        {/* Real-time Chat Drawer (WebRTC DataChannel) */}
        {chatOpen && (
          <aside className="w-80 h-[calc(100vh-64px)] glass-panel border-l border-slate-800 flex flex-col z-40 transition-all duration-300">
            <div className="p-4 border-b border-slate-800 flex items-center justify-between">
              <h3 className="font-semibold text-sm text-slate-200">DataChannel Chat</h3>
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
                placeholder="Type message via DataChannel..."
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
          }
        }}
        onToggleCam={() => {
          if (localStream) {
            localStream.getVideoTracks().forEach((t) => (t.enabled = !t.enabled));
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

