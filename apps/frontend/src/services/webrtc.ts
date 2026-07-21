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
          const offer = await this.pc.createOffer();
          await this.pc.setLocalDescription(offer);
          this.ws.send(JSON.stringify({ type: 'offer', sdp: offer.sdp }));
        }
      } catch (err) {
        console.error('Error during renegotiation', err);
      }
    };

    // Add local tracks to PeerConnection
    if (this.localStream) {
      this.localStream.getTracks().forEach((track) => {
        if (this.pc && this.localStream) {
          this.pc.addTrack(track, this.localStream);
        }
      });
    }

    // Offerer side DataChannel for Chat & File Transfer
    this.dataChannel = this.pc.createDataChannel('chat-and-files');
    this.setupDataChannelEvents(this.dataChannel);

    // Answerer side DataChannel listener
    this.pc.ondatachannel = (event) => {
      this.setupDataChannelEvents(event.channel);
    };

    const streamMetadataMap = new Map<string, string>();


    // Handle Remote Track
    this.pc.ontrack = (event) => {
      const stream = event.streams && event.streams[0] ? event.streams[0] : new MediaStream([event.track]);
      if (this.onTrackAdded) {
        const isScreen =
          stream.id.includes('screen') ||
          event.track.label.toLowerCase().includes('screen') ||
          streamMetadataMap.get(stream.id) === 'screen';
        this.onTrackAdded({
          id: event.track.id,
          peerID: stream.id || 'remote-peer',
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

    this.ws.onmessage = async (event) => {
      const msg = JSON.parse(event.data);
      if (msg.type === 'offer' && this.pc) {
        await this.pc.setRemoteDescription(new RTCSessionDescription({ type: 'offer', sdp: msg.sdp }));
        const answer = await this.pc.createAnswer();
        await this.pc.setLocalDescription(answer);
        this.ws?.send(JSON.stringify({ type: 'answer', sdp: answer.sdp }));
      } else if (msg.type === 'answer' && this.pc) {
        await this.pc.setRemoteDescription(new RTCSessionDescription({ type: 'answer', sdp: msg.sdp }));
      } else if (msg.type === 'candidate' && this.pc) {
        await this.pc.addIceCandidate(new RTCIceCandidate(msg.candidate));
      } else if (msg.type === 'track_metadata') {
        // BR4: Handle out-of-band screen track metadata from Pion SFU
        if (msg.stream_id && msg.kind) {
          streamMetadataMap.set(msg.stream_id, msg.kind);
        }
        if (msg.kind === 'screen' && msg.stream_id && this.onTrackAdded) {
          this.onTrackAdded({
            id: msg.stream_id,
            peerID: msg.peer_id || 'remote-presenter',
            stream: new MediaStream(),
            isScreenShare: true,
          });
        } else if (msg.kind === 'screen_stopped' && msg.stream_id && this.onTrackRemoved) {
          this.onTrackRemoved(msg.stream_id);
        }
      }
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
