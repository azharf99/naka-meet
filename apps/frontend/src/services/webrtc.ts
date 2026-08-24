import { getIceServers } from './auth';

// Used only if fetching this deployment's own ICE server list fails
// outright (network error) — not the steady-state behavior. The backend's
// /api/v1/ice-servers already returns this same entry when TURN isn't
// configured, so this is purely a fallback for "couldn't even reach the
// backend", keeping a connection attempt possible rather than failing
// immediately.
const FALLBACK_ICE_SERVERS: RTCIceServer[] = [{ urls: 'stun:stun.l.google.com:19302' }];

// acquireLocalMedia requests camera and microphone as two independent
// getUserMedia calls instead of one combined {video, audio} request. A
// single combined call fails BOTH devices if either one is busy/denied
// (e.g. the camera is held by another app) — a common, unremarkable
// situation that used to drop the participant to fully receive-only
// instead of just going camera-off or mic-off for the one device that
// actually failed. Each failure is caught independently so one busy
// device degrades only itself.
async function acquireLocalMedia(): Promise<MediaStream | null> {
  const [videoResult, audioResult] = await Promise.all([
    navigator.mediaDevices.getUserMedia({ video: { width: 1280, height: 720 } }).catch((err) => {
      console.warn('Could not access camera, proceeding without video', err);
      return null;
    }),
    navigator.mediaDevices.getUserMedia({ audio: true }).catch((err) => {
      console.warn('Could not access microphone, proceeding without audio', err);
      return null;
    }),
  ]);

  const tracks = [...(videoResult?.getTracks() ?? []), ...(audioResult?.getTracks() ?? [])];
  return tracks.length > 0 ? new MediaStream(tracks) : null;
}

export interface ParticipantTrack {
  id: string;
  peerID: string;
  // The raw UUID peer_id from track_metadata — distinct from peerID above,
  // which is actually the resolved display name (see the comment on
  // `peerID: peerName` in the ontrack handler below). Needed for
  // participant-targeting host actions like removal, where the REST
  // endpoint expects the real ID, not a display name that could collide or
  // change. Empty if metadata hasn't arrived yet for this track.
  rawPeerId: string;
  stream: MediaStream;
  isScreenShare: boolean;
}

export type TrackCallback = (track: ParticipantTrack) => void;
export type MessageCallback = (msg: { sender: string; text: string; time: string }) => void;
export type MediaStateCallback = (state: { peerId: string; kind: 'mic' | 'cam'; enabled: boolean }) => void;
export type RecordingStateCallback = (state: { active: boolean; kind: string }) => void;
export type PeerStaleCallback = (state: { peerId: string; kind: string; stale: boolean }) => void;

export class WebRTCService {
  private pc: RTCPeerConnection | null = null;
  private ws: WebSocket | null = null;
  private localStream: MediaStream | null = null;
  private screenStream: MediaStream | null = null;

  private makingOffer = false;
  private messageQueue: Promise<void> = Promise.resolve();
  private trackMetadataMap = new Map<string, { streamId: string; peerId: string; peerName: string; kind: string }>();
  private remoteStreams = new Map<string, MediaStream>();
  private peerIdentifiersMap = new Map<string, Set<string>>();

  private registerPeerIdentifier(peerId: string, identifier: string) {
    if (!peerId || !identifier) return;
    let set = this.peerIdentifiersMap.get(peerId);
    if (!set) {
      set = new Set<string>();
      this.peerIdentifiersMap.set(peerId, set);
    }
    set.add(identifier);
  }

  public onTrackAdded?: TrackCallback;
  public onTrackRemoved?: (trackId: string) => void;
  public onScreenShareEnded?: () => void;
  public onMessageReceived?: MessageCallback;
  public onMediaStateChanged?: MediaStateCallback;
  public onRecordingStateChanged?: RecordingStateCallback;
  // Fired when the server's RTP-liveness watchdog flags a publisher's track
  // as having gone silent (stale: true) or having recovered (stale: false)
  // — lets the grid show a "reconnecting" state instead of a permanently
  // frozen last frame with no explanation.
  public onPeerStaleChanged?: PeerStaleCallback;
  // Fired whenever the room's arbitrated active screen share changes — a
  // new share starting ("latest wins"), an active share stopping, or a
  // host's set_presentation pick/reclaim. activePeerId is '' when nobody
  // is presenting.
  public onPresentationStateChanged?: (state: { activePeerId: string; activePeerName: string }) => void;
  // Fired when THIS client is force-removed by the host (or by the
  // server's stale-timeout auto-removal) — distinct from onDisconnected so
  // the UI can show "You were removed" instead of a generic drop/reload
  // prompt. reason is "host_removed" or "stale_timeout".
  public onRemoved?: (reason: string) => void;
  // Fired when the host force-mutes this client (BR1). There's no
  // "force_unmute" — only this client's own action can turn its mic back
  // on, so the handler just needs to disable the local track and relay the
  // usual media_state update, same as if the user had muted themselves.
  public onForceMuted?: () => void;
  // wasConnected distinguishes a mid-call drop (WS reached `open` at least
  // once) from a connection that was refused outright — most commonly the
  // signaling WS being 403'd at upgrade because this origin isn't in the
  // server's ALLOWED_ORIGINS allowlist. Both look identical to the browser
  // (onclose fires either way) but call for very different user-facing
  // messages, so the distinction has to be tracked here.
  public onDisconnected?: (wasConnected: boolean) => void;
  // Set once a "removed" message arrives, so the subsequent onclose (the
  // server closes the connection right after sending it) doesn't also fire
  // the generic onDisconnected — the removal notice already explains why.
  private wasRemoved = false;

  constructor(private roomSlug: string) {}


  public async connectToken(token: string): Promise<void> {
    // 1. Get Local Media (Camera + Mic) and this deployment's ICE server
    // list (STUN, plus TURN if the operator has configured coturn — see
    // .env.example) in parallel — independent I/O with nothing to
    // serialize. STUN alone silently strands any participant whose network
    // needs a relay (symmetric NAT, most mobile carriers, restrictive
    // corporate firewalls) — invisible on a single LAN, which is exactly
    // why that class of failure is easy to ship without noticing.
    const [mediaResult, iceServers] = await Promise.all([
      acquireLocalMedia(),
      getIceServers(token)
        .then((res) => (res.iceServers.length > 0 ? res.iceServers : FALLBACK_ICE_SERVERS))
        .catch((err) => {
          console.warn('Could not fetch ICE server config, falling back to public STUN only', err);
          return FALLBACK_ICE_SERVERS;
        }),
    ]);
    this.localStream = mediaResult;

    // 2. Setup RTCPeerConnection
    this.pc = new RTCPeerConnection({ iceServers });

    // Handle Trickle ICE Candidates
    this.pc.onicecandidate = (event) => {
      if (event.candidate && this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(
          JSON.stringify({
            type: 'candidate',
            candidate: event.candidate,
          })
        );
      }
    };

    // Handle Negotiation Needed (SDP Renegotiation when screen track added)
    this.pc.onnegotiationneeded = async () => {
      try {
        if (this.pc && this.ws && this.ws.readyState === WebSocket.OPEN) {
          this.makingOffer = true;
          const offer = await this.pc.createOffer();
          if (this.pc.signalingState !== 'stable') return;
          await this.pc.setLocalDescription(offer);
          this.ws.send(JSON.stringify({ type: 'offer', sdp: offer.sdp }));
        }
      } catch (err) {
        console.error('Error during renegotiation', err);
      } finally {
        this.makingOffer = false;
      }
    };

    // Add local tracks to PeerConnection
    if (this.localStream && this.localStream.getTracks().length > 0) {
      this.localStream.getTracks().forEach((track) => {
        if (this.pc && this.localStream) {
          this.pc.addTrack(track, this.localStream);
        }
      });
    } else if (this.pc && typeof this.pc.addTransceiver === 'function') {
      try {
        this.pc.addTransceiver('video', { direction: 'recvonly' });
        this.pc.addTransceiver('audio', { direction: 'recvonly' });
      } catch (e) {
        console.warn('Could not add recvonly transceivers', e);
      }
    }

    const streamMetadataMap = new Map<string, string>();
    const peerNameMap = new Map<string, string>();

    // Handle Remote Track
    this.pc.ontrack = (event) => {
      const trackId = event.track.id;
      const metadata = this.trackMetadataMap.get(trackId);
      const streamId = metadata?.streamId || (event.streams && event.streams[0]?.id) || `stream-${trackId}`;
      const peerName = metadata?.peerName || peerNameMap.get(streamId) || streamId;
      const kind = metadata?.kind || streamMetadataMap.get(streamId) || 'camera';
      const isScreen =
        kind === 'screen' ||
        streamId.includes('screen') ||
        event.track.label.toLowerCase().includes('screen');

      let stream = this.remoteStreams.get(streamId);
      if (!stream) {
        if (event.streams && event.streams[0]) {
          stream = event.streams[0];
        } else {
          stream = new MediaStream();
          try {
            Object.defineProperty(stream, 'id', { value: streamId, configurable: true, enumerable: true });
          } catch (e) {
            console.warn('Failed to override MediaStream id', e);
          }
        }
        this.remoteStreams.set(streamId, stream);
      }
      
      if (stream && typeof stream.getTracks === 'function') {
        if (!stream.getTracks().find((t) => t.id === trackId)) {
          if (typeof stream.addTrack === 'function') {
            stream.addTrack(event.track);
          }
        }
      }

      const handleTrackEnd = () => {
        if (this.onTrackRemoved) {
          this.onTrackRemoved(trackId);
          if (peerName) this.onTrackRemoved(peerName);
        }
      };
      if (event.track) {
        event.track.onended = handleTrackEnd;
      }

      if (this.onTrackAdded) {
        this.onTrackAdded({
          id: trackId,
          peerID: peerName,
          rawPeerId: metadata?.peerId || '',
          stream: stream,
          isScreenShare: isScreen,
        });
      }
    };

    // Setup WebSocket Signaling
    const isHttps = typeof window !== 'undefined' && window.location.protocol === 'https:';
    const wsProtocol = isHttps ? 'wss:' : 'ws:';
    const wsHost = typeof window !== 'undefined' ? window.location.host : 'localhost:8080';
    const wsURL = `${wsProtocol}//${wsHost}/ws/signaling?room_slug=${this.roomSlug}&token=${encodeURIComponent(token)}`;

    this.ws = new WebSocket(wsURL);
    let wsDidOpen = false;

    this.ws.onopen = async () => {
      wsDidOpen = true;
      try {
        if (this.pc && this.ws && (this.ws.readyState === 1 || this.ws.readyState === WebSocket.OPEN)) {
          if (this.pc.signalingState === 'stable') {
            this.makingOffer = true;
            const offer = await this.pc.createOffer();
            await this.pc.setLocalDescription(offer);
            this.ws.send(JSON.stringify({ type: 'offer', sdp: offer.sdp }));
          }
        }
      } catch (err) {
        console.error('Error sending initial SDP offer on WS open:', err);
      } finally {
        this.makingOffer = false;
      }
    };

    this.ws.onclose = () => {
      // The server sends "removed" then closes the socket itself — that
      // close shouldn't also trigger the generic "Connection lost" banner,
      // which would misleadingly suggest reloading might fix it.
      if (this.wasRemoved) return;
      if (this.onDisconnected) this.onDisconnected(wsDidOpen);
    };
    this.ws.onerror = (err) => {
      console.error('Signaling WebSocket error', err);
    };

    this.ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      if (msg.type === 'track_metadata') {
        if (msg.stream_id && msg.kind) {
          streamMetadataMap.set(msg.stream_id, msg.kind);
        }
        if (msg.stream_id && (msg.peer_name || msg.peer_id)) {
          peerNameMap.set(msg.stream_id, msg.peer_name || msg.peer_id);
        }
        if (msg.peer_id) {
          if (msg.stream_id) this.registerPeerIdentifier(msg.peer_id, msg.stream_id);
          if (msg.track_id) this.registerPeerIdentifier(msg.peer_id, msg.track_id);
          if (msg.peer_name) this.registerPeerIdentifier(msg.peer_id, msg.peer_name);
        }
        if (msg.track_id && msg.stream_id) {
          this.trackMetadataMap.set(msg.track_id, {
            streamId: msg.stream_id,
            peerId: msg.peer_id || '',
            peerName: msg.peer_name || msg.peer_id || '',
            kind: msg.kind || 'camera',
          });
        }
        if (msg.kind === 'screen_stopped' && msg.stream_id && this.onTrackRemoved) {
          this.onTrackRemoved(msg.stream_id);
        }
        return;
      }

      if (msg.type === 'media_state') {
        // onTrackAdded's peerID is actually the resolved peerName (see
        // `peerID: peerName` below), not the raw peer_id — matching that
        // same convention here is required, or lookups keyed by
        // ParticipantTrack.peerID in VideoGrid would never match.
        const peerId = msg.peer_name || msg.peer_id;
        if (peerId && this.onMediaStateChanged) {
          this.onMediaStateChanged({
            peerId,
            kind: msg.media_kind,
            enabled: !!msg.enabled,
          });
        }
        return;
      }

      if (msg.type === 'peer_stale') {
        // Same peer_name-over-peer_id convention as media_state above, so
        // this keys into the same map VideoGrid looks up by track.peerID.
        const peerId = msg.peer_name || msg.peer_id;
        if (peerId && this.onPeerStaleChanged) {
          this.onPeerStaleChanged({ peerId, kind: msg.kind || '', stale: !!msg.stale });
        }
        return;
      }

      if (msg.type === 'presentation_state') {
        // active_peer_id/active_peer_name here are the raw ID + name, not
        // the peerName-as-peerID convention used elsewhere — this is
        // matched against ParticipantTrack.rawPeerId in VideoGrid, not
        // track.peerID, since arbitration targets a real participant, not
        // a display-name-keyed tile.
        if (this.onPresentationStateChanged) {
          this.onPresentationStateChanged({
            activePeerId: msg.active_peer_id || '',
            activePeerName: msg.active_peer_name || '',
          });
        }
        return;
      }

      if (msg.type === 'removed') {
        this.wasRemoved = true;
        if (this.onRemoved) this.onRemoved(msg.reason || 'host_removed');
        return;
      }

      if (msg.type === 'force_mute') {
        if (this.onForceMuted) this.onForceMuted();
        return;
      }

      if (msg.type === 'recording_state') {
        // Server-triggered (from the host's REST call, not this peer's own
        // WS connection) and broadcast to every participant — including the
        // host — so everyone gets the on-screen consent notice, not just
        // whoever clicked the button.
        if (this.onRecordingStateChanged) {
          this.onRecordingStateChanged({ active: !!msg.active, kind: msg.kind || '' });
        }
        return;
      }

      if (msg.type === 'chat') {
        if (this.onMessageReceived) {
          this.onMessageReceived({
            sender: msg.sender || 'Guest',
            text: msg.text || '',
            time: msg.time || new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
          });
        }
        return;
      }

      if (msg.type === 'participant_left' && msg.peer_id) {
        const identifiers = this.peerIdentifiersMap.get(msg.peer_id);
        if (this.onTrackRemoved) {
          this.onTrackRemoved(msg.peer_id);
          if (identifiers) {
            identifiers.forEach((id) => this.onTrackRemoved!(id));
          }
        }
        this.remoteStreams.delete(msg.peer_id);
        if (identifiers) {
          identifiers.forEach((id) => this.remoteStreams.delete(id));
        }
        this.peerIdentifiersMap.delete(msg.peer_id);
        return;
      }

      return (this.messageQueue = this.messageQueue
        .then(async () => {
          if (msg.type === 'offer' && this.pc) {
            try {
              const readyStateStable = this.pc.signalingState === 'stable';
              const offerCollision = this.makingOffer || !readyStateStable;
              if (offerCollision && this.pc.signalingState === 'have-local-offer') {
                await this.pc.setLocalDescription({ type: 'rollback' });
              }
              await this.pc.setRemoteDescription(new RTCSessionDescription({ type: 'offer', sdp: msg.sdp }));
              const answer = await this.pc.createAnswer();
              await this.pc.setLocalDescription(answer);
              this.ws?.send(JSON.stringify({ type: 'answer', sdp: answer.sdp }));
            } catch (err) {
              console.error('Error handling remote offer:', err);
            }
          } else if (msg.type === 'answer' && this.pc) {
            try {
              await this.pc.setRemoteDescription(new RTCSessionDescription({ type: 'answer', sdp: msg.sdp }));
            } catch (err) {
              console.error('Error handling remote answer:', err);
            }
          } else if (msg.type === 'candidate' && this.pc) {
            try {
              await this.pc.addIceCandidate(new RTCIceCandidate(msg.candidate));
            } catch (err) {
              console.error('Error adding remote ICE candidate:', err);
            }
          }
        })
        .catch((err) => {
          console.error('Error processing SDP message:', err);
        }));
    };
  }

  public async startScreenShare(): Promise<MediaStream | null> {
    try {
      this.screenStream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: true });
      if (this.screenStream) {
        this.screenStream.getTracks().forEach((track) => {
          track.onended = () => {
            this.stopScreenShare();
          };
          if (this.pc && this.screenStream) {
            this.pc.addTrack(track, this.screenStream);
          }
        });
      }

      // Out-of-band track metadata notification over WebSocket (API.md spec)
      if (this.ws && this.ws.readyState === WebSocket.OPEN && this.screenStream) {
        this.ws.send(
          JSON.stringify({
            type: 'track_metadata',
            stream_id: this.screenStream.id,
            kind: 'screen',
          })
        );
      }

      return this.screenStream;
    } catch (err) {
      console.error('Screen share error', err);
      return null;
    }
  }

  public stopScreenShare(): void {
    if (this.screenStream) {
      const streamId = this.screenStream.id;
      this.screenStream.getTracks().forEach((t) => {
        if (t && typeof t.stop === 'function') {
          t.stop();
        }
      });
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(
          JSON.stringify({
            type: 'track_metadata',
            stream_id: streamId,
            kind: 'screen_stopped',
          })
        );
      }
      this.screenStream = null;
    }
    if (this.onScreenShareEnded) {
      this.onScreenShareEnded();
    }
  }

  // Notifies other participants that this peer muted/unmuted its mic or
  // turned its camera on/off. A sender toggling its own local track's
  // `.enabled` produces no observable event on the corresponding remote
  // track on other peers' PeerConnections, so this out-of-band message is
  // the only way other participants' tiles can reflect it.
  public sendMediaState(kind: 'mic' | 'cam', enabled: boolean): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify({ type: 'media_state', media_kind: kind, enabled }));
  }

  // Host-only screen-share arbitration override: pick any currently-sharing
  // participant (by raw peer ID) as the room's active presentation,
  // including the host's own ID to reclaim a share that got suspended by
  // someone else's. Authorization is enforced server-side (the server
  // silently ignores this from a non-host or for a peer who isn't actually
  // sharing) — this just sends the request.
  public setPresentation(peerId: string): void {
    if (!peerId || !this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify({ type: 'set_presentation', peer_id: peerId }));
  }

  // Chat is relayed by the SFU over the signaling WebSocket, not a
  // browser-to-browser RTCDataChannel: every client only ever has a
  // PeerConnection to the SFU (hub-and-spoke), never to other participants,
  // so a DataChannel message would have nowhere to go without server relay.
  public sendMessage(text: string): void {
    const trimmed = text.trim();
    if (!trimmed || !this.ws || this.ws.readyState !== WebSocket.OPEN) return;

    this.ws.send(JSON.stringify({ type: 'chat', text: trimmed }));

    if (this.onMessageReceived) {
      this.onMessageReceived({
        sender: 'You',
        text: trimmed,
        time: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      });
    }
  }

  public getLocalStream(): MediaStream | null {
    return this.localStream;
  }

  public disconnect(): void {
    this.localStream?.getTracks().forEach((t) => t.stop());
    this.screenStream?.getTracks().forEach((t) => t.stop());
    this.pc?.close();
    this.ws?.close();
  }
}
