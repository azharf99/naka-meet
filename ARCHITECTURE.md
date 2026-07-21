# System Architecture (Monorepo Microservices)

Sistem menggunakan topologi *Monorepo* yang diorkestrasi menggunakan `docker-compose.yml` yang bisa dilihat di file `Docker Orchestration.md`.

## Komponen 1: Golang SFU Backend (`apps/sfu`)
- **Fungsi:** Pusat *routing* lalu lintas WebRTC dan *Signaling* WebSocket.
- **Karakteristik:** Latensi rendah, komputasi ringan (I/O *bound*).
- **Porting:** Membuka port 8080 (TCP) untuk HTTP/WS dan merentangkan port 50000-50050 (UDP) untuk paket RTP/RTCP klien WebRTC. Dibuat dengan *multi-stage Docker build* berbasis Alpine.

## Komponen 2: Node.js Egress Worker (`apps/egress`)
- **Fungsi:** Bertindak sebagai *bot/headless client* yang merekam layar dan melakukan *transcoding* video.
- **Karakteristik:** Komputasi sangat berat (CPU/Memory *bound*).
- **Teknologi:** Menggunakan image `node:20-bookworm-slim` yang dilengkapi instalasi Chromium, Xvfb (*Virtual Display* di `:99`), dan FFmpeg. 
- **Isolasi:** Dibatasi maksimal 1.5 CPU dan 1GB RAM oleh Docker Compose untuk melindungi stabilitas *host*.

## Komponen 3: Frontend Web Client (`apps/frontend`)
- **Fungsi:** Antarmuka React yang berjalan di *browser* klien.

## Komponen 4: Message Broker (Redis)
- **Fungsi:** Jembatan komunikasi antar-layanan (SFU tidak pernah memanggil *Egress Worker* secara langsung).


## Struktur Direktori (Monorepo)

Gunakan arsitektur **Monorepo** dengan bantuan `docker-compose.yml` di akar (*root*) proyek. Ini sangat ideal untuk dikembangkan secara solo atau dalam tim kecil.

```text
gothub-meet/                  # Root Repository Git
├── apps/
│   ├── sfu/                  # Layanan 1: Golang WebRTC
│   │   ├── main.go
│   │   ├── go.mod
│   │   └── Dockerfile        # Menggunakan image dasar Golang alpine (Kecil)
│   │
│   ├── egress/               # Layanan 2: Node.js + FFmpeg
│   │   ├── worker.js
│   │   ├── package.json
│   │   └── Dockerfile        # Menggunakan image dasar Ubuntu/Node + apt-get install ffmpeg
│   │
│   └── frontend/             # Layanan 3: React UI
│       ├── src/
│       └── package.json
│
├── shared/                   # (Opsional) Protobuf atau definisi struktur JSON yang dipakai bersama
│
└── docker-compose.yml        # Orkestrasi lokal yang menjalankan semuanya sekaligus

```