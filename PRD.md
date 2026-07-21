# Product Requirements Document: Naka Meet

**Lead Architect:** Azhar Faturohman Ahidin

## 1. Visi Produk
Menyediakan platform *video conference* mandiri (*self-hosted*) berkinerja tinggi menggunakan Golang, dirancang khusus untuk sesi mentoring *online*, tutorial pemrograman *full-stack*, dan kolaborasi tim dengan latensi sangat rendah.

## 2. Target Pengguna
- Kreator konten edukasi teknologi (seperti saluran Naka atau HikerCode) yang membutuhkan platform andal untuk *live coding*.
- Pengajar yang membutuhkan integrasi *screen sharing* dengan teks koding yang tetap tajam (melalui kontrol *Simulcast bitrate/framerate*).
- *Broadcaster* yang membutuhkan fitur dorongan RTMP langsung ke YouTube.

## 3. Fitur Utama (Core Features)
- **Room Management:** Pembuatan ruang rapat persisten dengan UUID/Slug.
- **Video & Audio:** Topologi SFU dengan latensi < 200ms.
- **Screen Sharing:** Dukungan *multi-track* (kamera + layar bersamaan) dengan manajemen UI yang dinamis.
- **Adaptasi Jaringan (Simulcast):** Distribusi *spatial/temporal layers* otomatis menyesuaikan *bandwidth* penonton.
- **Real-time Chat & File Transfer:** Pengiriman pesan teks dan *chunking file* murni via WebRTC DataChannel (tanpa WebSocket tambahan).
- **On-Demand Egress:** Perekaman rapat dan *Live Streaming* RTMP menggunakan *headless worker* yang terisolasi.