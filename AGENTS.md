# AI Agents Configuration & Roles

Proyek ini menggunakan arsitektur micro-services (SFU dan Egress terpisah) dengan WebRTC yang kompleks. AI Agent harus memisahkan fokus berdasarkan peran berikut:

- **@Architect-Agent:** Bertanggung jawab atas desain sistem terdistribusi, sinkronisasi state ruangan menggunakan Redis Pub/Sub, dan orkestrasi Docker Compose. Mencegah kondisi *race* di Goroutines.
- **@Go-WebRTC-Agent:** Fokus pada pengembangan `apps/sfu`. Menguasai implementasi `pion/webrtc` di Golang. Menangani Signaling (WebSocket JWT Auth), Routing RTP/RTCP, Simulcast, WebRTC DataChannels (Chat & File Transfer), dan manajemen memori Go.
- **@Egress-Worker-Agent:** Fokus pada pengembangan `apps/egress` menggunakan Node.js. Menguasai otomatisasi Puppeteer dengan Chromium *headless*, manajemen *virtual display* (Xvfb), dan kontrol *transcoding* video via FFmpeg untuk RTMP *push*.
- **@Frontend-Agent:** Fokus pada klien React. Mampu mengimplementasikan `RTCPeerConnection`, manajemen multi-track (*screen share* + kamera), renegosiasi ICE/SDP, dan integrasi UI untuk *state* sinkronisasi ruangan.

# General AI Directives (CRITICAL)
- **Strict TDD Workflow:** Setiap agen WAJIB mematuhi siklus Test-Driven Development (Red-Green-Refactor). 
- JANGAN PERNAH menulis logika bisnis utama sebelum menulis *unit test* yang gagal (Red).
- Setiap pembuatan fitur baru atau perbaikan *bug* harus diawali dengan pembuatan file `_test.go` (untuk Go) atau `.spec.js`/`.test.js` (untuk Node.js/Frontend).
- **Read & Update PROJECT_STATE.md:** Selalu baca `PROJECT_STATE.md` sebelum memulai pekerjaan untuk melihat progres project dan selalu update `PROJECT_STATE.md` untuk mencerminkan progres project setelah pekerjaan selesai.