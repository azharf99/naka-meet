# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Naka Meet ("gothub-meet") is a self-hosted Zoom-style video conferencing app: a Golang SFU (Selective Forwarding Unit) using Pion WebRTC, a React/Vite frontend, and a Node.js/Puppeteer/FFmpeg egress worker for recording and RTMP live streaming. It's a monorepo of three independently-built, independently-tested services orchestrated by `docker-compose.yml`. Developed/tested on Windows + WSL2 + Docker only.

## Commands

### Go SFU backend (`apps/sfu`)
```bash
cd apps/sfu
go build ./...
go test -v -race ./...              # -race is mandatory: heavy use of goroutines + shared maps
go test -v -race ./pkg/webrtc/...   # single package
go test -v -race -run TestName ./pkg/room/...  # single test
```

### Frontend (`apps/frontend`)
```bash
cd apps/frontend
npm run dev       # vite dev server
npm run build     # tsc -b && vite build
npm run lint      # eslint .
npm test          # vitest run
```

### Egress worker (`apps/egress`)
```bash
cd apps/egress
npm start          # node worker.js
npm test           # node --test worker.test.js
```

### Full stack (Docker)
```bash
docker-compose up --build
```
Services: `postgres` (5432), `redis` (6379), `sfu-backend` (8080 HTTP/WS + 50000-50050/udp RTP), `frontend` (3000, nginx), `egress-worker` (no exposed port, capped at 1.5 CPU / 1GB RAM). `sfu-backend` waits on healthy redis+postgres; `egress-worker` waits on redis+frontend. Recordings persist to host `./recordings` via bind mount.

Required env vars (see `.env`, not committed): `JWT_SECRET`, `REDIS_URL`, `DB_DSN`, `POSTGRES_USER`/`POSTGRES_PASSWORD`/`POSTGRES_DB`, `PORT`, `WEBRTC_UDP_PORT_MIN`/`MAX`, `FRONTEND_URL`, `DISPLAY`.

## Architecture

### Topology: everything is hub-and-spoke through the SFU, never P2P mesh
Every client (host or guest) opens exactly one `RTCPeerConnection` to the Go SFU (`apps/frontend/src/services/webrtc.ts` → `WebRTCService`). The SFU is the only party each browser ever negotiates SDP/ICE with. Media fan-out, chat, and screen-share visibility are all mediated by the Go backend — there is no direct browser-to-browser link. This matters when debugging "participant can't see/hear/chat with another participant": the bug is almost always in how the SFU relays state between the two independent client↔SFU legs, not in browser-to-browser signaling.

### Go SFU (`apps/sfu/pkg`)
- `webrtc/router.go` — `SFURouter`: owns one `pion.PeerConnection` per connected user (`peers` map), tracks which room each peer is in (`peerRooms`), and stores published tracks per room (`roomTracks`, a `[]*RoomTrack` with `PublisherID`/`PublisherName`/`Kind`/the `pion.TrackLocal`). Two ways tracks reach subscribers: `SubscribePeerToRoomTracks` (pull, used once when a new peer's initial offer/answer completes, to catch them up on pre-existing tracks) and `BroadcastTrackAndRenegotiateWithMetadata` (push, used when an existing peer publishes a new track — triggers a fresh SDP offer to every other peer in the room). Both paths matter for "host can't see all participants": if either is broken, a join ordering edge case leaves someone's grid stale until next renegotiation.
- `signaling/handler.go` — `Handler.ServeHTTP` is the entire WS lifecycle for `/ws/signaling`: JWT extraction (cookie → `?token=` → `?jwt_token=` → `Authorization: Bearer`), room join via `RoomManager`, `SFURouter.AddPeer`, then a blocking read loop dispatching `offer`/`answer`/`candidate`/`track_metadata` messages. `SafeConn` wraps `*websocket.Conn` with a mutex because gorilla/websocket panics on concurrent writes — multiple goroutines (`OnICECandidate`, `OnTrack`'s RTP forwarder, renegotiation) write to the same connection. `pc.OnTrack` classifies a track as `screen` vs `camera` by substring-matching `"screen"` in the track/stream ID (not by an explicit flag from the client's initial offer), broadcasts `track_metadata` immediately, then calls `AddTrackAndRenegotiateWithMetadata` to fan it out.
- **There is no `OnDataChannel`/chat relay in the Go backend.** The frontend creates an `RTCDataChannel` on its client↔SFU `PeerConnection` (`webrtc.ts` `connectToken`), but the Go side never reads from or forwards on any data channel — a message sent via `sendMessage` only reaches the SFU's own peer connection object and goes nowhere from there. This is the root architectural gap behind "chat not working"; fixing it requires either the Go SFU actively relaying DataChannel bytes between the room's peer connections, or replacing DataChannel chat with a WebSocket-broadcast message type (the signaling channel already has a working room broadcast: `Handler.broadcastToRoom`).
- `room/room_manager.go` — `RoomManager` holds rooms/participants in an in-memory `map` guarded by `sync.RWMutex` (`RWMutexMap`), optionally mirrored to Redis (`HSet`/`HDel` on `room:{slug}:participants`) for observability — Redis is not the source of truth for signaling routing. `MaxRoomParticipants = 50` is a hard cap. Disconnect uses a 15s grace `time.Timer` (`HandleDisconnect`) before actually removing a participant, to survive brief reconnects (per FR3) — this timer is why a participant that reloads the page can briefly "double" appear.
- `api/handler.go` — plain `http.Handler` doing manual path/method matching (no router library) for `/api/v1/auth/login`, `/api/v1/rooms` (POST, host-only), `/api/v1/rooms/:slug` (GET), `/api/v1/rooms/:slug/live` (POST, host-only — publishes to Redis `channel:egress_commands`). Host-only checks are `claims.Role != "host"` from the JWT, not room ownership.
- `db/models.go` — GORM models auto-migrated (`users`, `rooms`, `recordings`) if `DB_DSN` is set; the app degrades to Redis/in-memory-only state if Postgres is unreachable (see `main.go`'s warning log), so persistent room/recording history is optional infrastructure, not required for a call to function.

### Frontend (`apps/frontend/src`)
- `services/webrtc.ts` — `WebRTCService` is the single class owning the peer connection, WS, local/screen streams, and per-track/per-stream metadata maps (`trackMetadataMap`, `remoteStreams`, `peerIdentifiersMap`). Implements "polite peer" glare handling (`makingOffer` flag + rollback on collision) and a `messageQueue` promise chain so `offer`/`answer`/`candidate` WS messages are processed strictly in order — without this queue, an `answer` racing a `renegOffer` on join corrupts signaling state. Screen-share vs camera classification on the receiving end depends on `track_metadata` messages arriving before/alongside the corresponding `ontrack` event; when metadata is missing, it falls back to substring-matching `"screen"` in the stream ID or track label.
- `components/VideoGrid.tsx` — `deduplicateTracks` collapses multiple track entries keyed by `stream.id || peerID || id`, preferring a video-carrying entry over an audio-only placeholder for the same peer — this is what prevents duplicate/empty tiles, and is a common source of "can't see all participants" if a peer's stream ID changes between camera-only and camera+screen states. `getGridClass` picks the responsive column count from tile count. Stage Mode (BR4) is triggered purely by `activePresentationStream` being non-null (local or remote screen track present) and renders a 3:1 split layout instead of the grid.
- `App.tsx` — also handles the **egress role**: when loaded with `?room=<slug>&role=egress`, it auto-joins as a headless recorder identity (`handleJoinRoom('Egress Recorder', ...)`). This is the page Puppeteer drives inside the egress container — whatever this page renders on screen at that moment is exactly what FFmpeg captures. So "recording doesn't capture the full grid / screen share" is a frontend rendering bug in this same `VideoGrid`/Stage Mode path under the egress route, not a separate recording-specific code path.

### Egress worker (`apps/egress/worker.js`)
Subscribes to Redis `channel:egress_commands`, on `START_RECORDING`/`START_RTMP` launches headless-ish Chromium via Puppeteer (`--display=:99`, pointed at the frontend with `?role=egress`) and, independently, spawns `ffmpeg -f x11grab` against the same `DISPLAY` (`:99`, an Xvfb virtual framebuffer started by the container's `start.sh`) to capture whatever is on that virtual screen — FFmpeg has no awareness of DOM/video-element state, it's a dumb screen scraper of the X11 display. RTMP output uses `-f flv`; local recording writes `.mp4` under `RECORDINGS_DIR`. Auto-stops after 5 minutes idle (BR2) and always signals FFmpeg with `SIGINT` (never `SIGKILL`) so MP4/FLV container metadata is finalized correctly — killing it harder produces unplayable files.

### Signaling message types (WS `/ws/signaling`)
`offer` / `answer` / `candidate` (standard SDP/ICE), `track_metadata` (out-of-band `{stream_id, kind, peer_id, peer_name}` — `kind` is `camera`, `screen`, or `screen_stopped`), `participant_left` (server-pushed on disconnect, drives frontend tile cleanup). See `API.md` for the REST surface.

## Docs worth reading before large changes
`ARCHITECTURE.md`, `API.md`, `BUSINESS_RULES.md` (BR1-BR4, referenced by code comments), `REQUIREMENTS.md` (FR/NFR, referenced by code comments), `DATABASE.md`, `TESTING_STRATEGY.md` (mocking conventions: never open real UDP/network sockets in Go tests, use `ioredis-mock` for Redis, mock `child_process.spawn` for FFmpeg, stub `getUserMedia`/`getDisplayMedia` in frontend tests), `PROJECT_STATE.md` (living changelog of what's been fixed — check it for recent fixes before re-diagnosing an issue), `AGENTS.md` (role split: Architect/Go-WebRTC/Egress-Worker/Frontend agents, mandatory TDD red-green-refactor).
