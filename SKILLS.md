# Required Skills & Technical Context

AI Agent yang bekerja pada repositori ini harus menerapkan pedoman teknis berikut:

1. **Golang Concurrency:** Dilarang menggunakan `map` standar untuk *shared-state*. Wajib menggunakan `sync.RWMutex` atau *Go Channels* untuk manajemen partisipan di memori.
2. **WebRTC Protocol:** Memahami alur SDP Offer/Answer, ICE Candidates (*Trickle ICE*), RTP (*Media Transport*), dan RTCP (*Control Protocol* seperti PLI/FIR).
3. **Pion Framework:** Menggunakan `TrackRemote` (Ingress) dan `TrackLocalStaticRTP` (Egress). Wajib menangani kebocoran Goroutine saat klien *disconnect*.
4. **Node.js & Media:** Pemahaman mendalam tentang *Stream* di Node.js, eksekusi proses eksternal (`child_process`), dan parameter dasar FFmpeg (`libx264`, `aac`, `-f flv`).
5. **WebSocket Security:** Proses *Upgrade* HTTP ke WebSocket harus divalidasi menggunakan JWT via `HttpOnly Cookie` atau sistem *Ticket* (sekali pakai).


## Testing Ecosystem & Paradigms

1. **Golang Testing Stack:** Penguasaan penuh terhadap *package* bawaan `testing`, serta `stretchr/testify` (khususnya sub-package `assert`, `require`, dan `mock`).
2. **WebRTC Mocking:** Mampu membuat *mock* untuk *interface* WebRTC (seperti memalsukan SDP Offer atau *mocking* aliran paket RTP di Pion) tanpa perlu membuka koneksi UDP sungguhan di CI/CD.
3. **Concurrency Testing:** Wajib menggunakan *flag* `-race` saat menjalankan *test* di Go untuk mendeteksi *race condition* pada `sync.RWMutex` dan *Go Channels*.
4. **Node.js/Egress Testing:** Menggunakan framework `Jest` atau `Vitest` untuk menguji logika antrean Redis dan manipulasi argumen FFmpeg, serta kemampuan melakukan *mocking* terhadap *child_process*.