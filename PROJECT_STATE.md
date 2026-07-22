# Naka Meet Project State

## Status Saat Ini
- **Fase:** Seluruh Audit Audit Fase 1, 2, 3, & 4 Complete (100% Selesai & Verifikasi Lulus)
- **Terakhir Diperbarui:** 2026-07-22

## Komponen & Modul
- [x] Rencana Implementasi & Persetujuan Spesifikasi
- [x] Monorepo Structure (`apps/sfu`, `apps/egress`, `apps/frontend`)
- [x] `docker-compose.yml` (Redis 7, Postgres 16, Go SFU 1.26, Node Egress 24, Frontend Nginx 3000)
- [x] **Audit Fase 1 (SFU Backend):** REST Auth JWT (UUID v7 & Display Name), Dynamic Room Slug & Creation (`POST /api/v1/rooms`), Participant Count (`GET /api/v1/rooms/:slug`), CORS, Egress Trigger/Stop Commands (Role Host Only check), Pion SFU Router UDP 50000-50050, WebSocket Signaling handler with display name persistence (100% Tests Pass)
- [x] **Audit Fase 2 (Frontend React):** Vite + React 19 + TypeScript + Tailwind CSS v4 + Multi-User Lobby Dashboard (`Lobby.tsx`) + Guest Mode Join / Host Mode Create Room + Trickle ICE candidate + `ondatachannel` receiver + SDP Renegotiation + Stage Mode BR4 + Vitest Unit Tests (100% Tests Pass)
- [x] **Audit Fase 3 (Node.js Egress Worker):** Puppeteer Headless + FFmpeg RTMP FLV format support + Audio fallback (lavfi anullsrc) + `channel:egress_status` pubsub + 5-min auto-stop BR2 + Unit Tests (4/4 Tests Pass)
- [x] **Audit Fase 4 (Docker & E2E Integration):** PostgreSQL auto-migration GORM (`users`, `rooms`, `recordings` dengan UUID v7), Nginx Reverse Proxy, full container dependency healthchecks, & End-to-End verification.

- [x] **Perbaikan Dynamic Layout, Persistent Storage, RTMP Live Streaming & BR4 Stage Mode (100% TDD Lulus):**
  - **Dynamic Video Grid:** Deduplikasi track peserta dan penyesuaian CSS grid dinamis (1 tile per peserta, 1/2/4/9/10+ responsive layout).
  - **Persistent Recordings Storage:** Direct host bind mount `./recordings:/usr/src/app/recordings` pada `docker-compose.yml`, pembuatan folder `RUN mkdir -p recordings` di `Dockerfile`, serta pengaitan `RECORDINGS_DIR=/usr/src/app/recordings` di Egress Worker sehingga file rekaman MP4 tersimpan secara langsung ke direktori lokal host (`./recordings`).

  - **Pemisahan Record Room & Live Stream RTMP:** Tombol khusus "Record" (rekaman lokal) & "Go Live" (RTMP YouTube) dengan Modal Setup RTMP Ingestion URL.
  - **Audit & Solusi Akar Masalah WebRTC Multi-User & Concurrency (100% Verifikasi Lulus & Deployed):**
    1. **Structured RoomTrack & Metadata Persistence:** Refactor `SFURouter` (`RoomTrack`) untuk menyimpan `PublisherID`, `PublisherName`, dan `Kind` (`camera` vs `screen`) disertai proteksi filter agar publisher tidak berlangganan ke track miliknya sendiri (*self-track skipping*).
    2. **Pre-existing Tracks Renegotiation on Join:** Setelah partisipan baru menyelesaikan *SDP Offer/Answer* awal, server (`handler.go`) otomatis melanggan partisipan ke seluruh track aktif di ruangan, mengirim `track_metadata`, dan memicu renegosiasi SDP Offer baru agar video peserta sebelumnya langsung tampil di layar peserta baru.
    3. **SafeConn Thread-Safe WebSocket Routing:** Implementasi `SafeConn` dengan `sync.Mutex` untuk mencegah *gorilla/websocket concurrent write panic* dari *goroutine* (`OnICECandidate`, `OnTrack`, dan siklus renegosiasi).
    4. **Frontend Display Name & Video Deduplication:** Pemetaan `peerNameMap` & `streamMetadataMap` di `webrtc.ts` serta pembaruan logika `deduplicateTracks` di `VideoGrid.tsx` agar video aktif menggantikan entri audio-only tanpa menghasilkan kotak hitam/empty `MediaStream`.

## Log Aktivitas Terakhir
- **2026-07-22:** Penyelesaian perbaikan arsitektur SFU dan sinkronisasi WebRTC Multi-User (*Pre-existing Tracks Renegotiation*, *RoomTrack Metadata Persistence*, *SafeConn Thread-Safety*, dan *Frontend Display Name Mapping*). Seluruh unit test Go & React lulus 100% dan kontainer Docker (`naka-sfu`, `naka-frontend`) telah diperbarui dan berjalan stabil.
- **2026-07-22 (Fix Multi-Room Fan-Out & Renegotiation Glare):**
  - **Backend Room-Scoped Fan-Out**: Membatasi fan-out track RTP dan distribusi SDP Offer hanya untuk peer di room yang sama melalui pemetaan `peerRooms` di `SFURouter`, menyelesaikan isu "inbound-rtp" hilang akibat kerusakan state negosiasi lintas room.
  - **Frontend Polite Renegotiation**: Menerapkan pattern perfect negotiation (polite rollback) di `WebRTCService` untuk menangani tabrakan SDP offer (glare) saat in-meeting.
  - **Dynamic Track Rendering**: Menambahkan event listener `addtrack` dan `removetrack` pada `MediaStream` di `VideoTile` untuk memaksa re-binding `srcObject` saat track baru ditambahkan dinamis.





