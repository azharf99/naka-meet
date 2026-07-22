export interface ParticipantTrack {
  id: string;
  peerID: string;
  stream: MediaStream;
  isScreenShare: boolean;
}

export type TrackCallback = (track: ParticipantTrack) => void;
export type MessageCallback = (msg: { sender: string; text: string; time: string }) => void;

export class WebRTCService {
  private pc: RTCPeerConnection | null = null;
  private ws: WebSocket | null = null;
  private dataChannel: RTCDataChannel | null = null;
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

  constructor(private roomSlug: string) {}


  public async connectToken(token: string): Promise<void> {
    // 1. Get Local Media (Camera + Mic)
    try {
      this.localStream = await navigator.mediaDevices.getUserMedia({
        video: { width: 1280, height: 720 },
        audio: true,
      });
    } catch (err) {
      console.warn('Could not access media devices, proceeding audio/video muted', err);
    }

    // 2. Setup RTCPeerConnection
    this.pc = new RTCPeerConnection({
      iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
    });

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

    // Offerer side DataChannel for Chat & File Transfer
    this.dataChannel = this.pc.createDataChannel('chat-and-files');
    this.setupDataChannelEvents(this.dataChannel);

    // Answerer side DataChannel listener
    this.pc.ondatachannel = (event) => {
      this.setupDataChannelEvents(event.channel);
    };

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

    this.ws.onopen = async () => {
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

  private setupDataChannelEvents(dc: RTCDataChannel) {
    dc.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (this.onMessageReceived) {
          this.onMessageReceived(data);
        }
      } catch (err) {
        console.error('Failed to parse DataChannel message', err);
      }
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



  public sendMessage(text: string): void {
    if (this.dataChannel && this.dataChannel.readyState === 'open') {
      const payload = {
        sender: 'You',
        text: text,
        time: new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      };
      this.dataChannel.send(JSON.stringify(payload));
      if (this.onMessageReceived) {
        this.onMessageReceived(payload);
      }
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
