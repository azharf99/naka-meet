# Requirements Specification

## Functional Requirements (FR)
- **FR1 (Auth):** Sistem mengautentikasi klien *Signaling* menggunakan JWT sebelum proses *Upgrade* protokol HTTP terjadi.
- **FR2 (Media Routing):** Backend SFU harus menduplikasi aliran paket RTP satu klien (Fan-out) ke seluruh peserta lain di ruangan yang sama.
- **FR3 (Network Resilience):** Server SFU harus menahan *state* WebRTC (Pion) selama 15 detik (menggunakan `time.Timer`) ketika koneksi WebSocket terputus untuk memberikan jeda rekoneksi.
- **FR4 (Egress Trigger):** Sistem harus menembakkan perintah *Egress* ke antrean Redis Pub/Sub, yang kemudian ditangkap oleh Egress Worker Node.js.

## Non-Functional Requirements (NFR)
- **NFR1 (Resource Isolation):** Proses *transcoding* FFmpeg dan perenderan antarmuka Chrome wajib dijalankan di *container* terpisah dengan batasan CPU dan RAM yang ketat.
- **NFR2 (Memory Management):** Keduanya (Go dan Node.js) harus menangani *Garbage Collection* secara proaktif untuk mencegah *Out-of-Memory* (OOM).
- **NFR3 (Scalability):** *Signaling Gateway* dan *State* ruangan dikelola menggunakan Redis agar siap bermigrasi ke topologi *Distributed SFU*.

## Testing Requirements (TR)
- **TR1 (Unit Test Coverage):** *Code coverage* minimal untuk komponen logika bisnis (seperti `RoomManager`, JWT Validator, dan WebRTC Router) adalah **85%**.
- **TR2 (Integration Tests):** Wajib ada pengujian integrasi yang memvalidasi aliran pesan antara Go SFU dan Redis Pub/Sub (menggunakan Redis Testcontainers atau Mock Redis).
- **TR3 (Continuous Integration Ready):** Semua *test suite* harus dirancang sedemikian rupa agar dapat berjalan di lingkungan CI/CD *headless* secara deterministik (tidak boleh ada *flaky tests* akibat fungsi `time.Sleep`).