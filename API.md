# API Specification

## REST API (Base: `/api/v1`)
- **POST `/auth/login`**
  - Body: `{email, password}`
  - Response: Header `Set-Cookie: jwt_token=...; HttpOnly; Secure`
- **POST `/rooms/:slug/live`**
  - Auth: Harus Host ruangan.
  - Body: `{stream_key: "youtube-key-123"}`
  - Action: Mempublikasikan event ke Redis `channel:egress_commands`.

## WebSocket Signaling (Base: `/ws`)
- **GET `/ws/signaling?session_id=...`**
  - *Connection Upgrade*: Divalidasi otomatis oleh server Golang melalui *cookie* bawaan *browser*.
- **Data Payload (Format JSON):**
  - `{"type": "offer", "sdp": "..."}` 
  - `{"type": "answer", "sdp": "..."}`
  - `{"type": "candidate", "candidate": {...}}`
  - `{"type": "track_metadata", "stream_id": "...", "kind": "screen"}` (Out-of-band labeling untuk multi-track).