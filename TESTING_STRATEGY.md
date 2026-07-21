# Testing Strategy & TDD Guidelines: Gothub Meet

Dokumen ini mendefinisikan strategi pengujian (Testing Strategy) dan pola *mocking* yang wajib digunakan oleh seluruh *engineer* dan AI Agent dalam menerapkan Test-Driven Development (TDD) di proyek ini.

## 1. Filosofi TDD (Red-Green-Refactor)
- **Kewajiban:** Dilarang menulis implementasi logika bisnis sebelum menulis *test case* yang gagal.
- **Fokus Perilaku:** Uji *behavior* (hasil akhir) dari sebuah fungsi, bukan *internal state*-nya.
- **Isolasi Eksternal:** Layanan pihak ketiga (seperti jaringan UDP murni, proses eksternal FFmpeg, atau *hardware* kamera) WAJIB di-*mock* pada level *Unit Test*.

---

## 2. Strategi Pengujian Golang SFU (Backend)

Komponen inti SFU sarat dengan konkurensi dan manipulasi paket RTP. Pengujian dilakukan menggunakan *package* bawaan `testing` dan `stretchr/testify`.

### 2.1. Mendeteksi Race Condition
Setiap *test* yang melibatkan Goroutines (seperti proses `BroadcastRTP` atau manajemen *map* `Participants`) wajib dijalankan dengan *flag* pendeteksi balapan memori:
```bash
go test -v -race ./...

```

### 2.2. Mocking Pion WebRTC

Jangan pernah membuat koneksi jaringan P2P/UDP sungguhan di dalam unit test.

* **Mocking Track (Ingress/Egress):** Untuk menguji fungsi *router* RTP tanpa klien asli, gunakan objek statis dari Pion.
```go
// Contoh pembuatan Mock Track di dalam Test
mockTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
assert.NoError(t, err)

// Masukkan mockTrack ini ke dalam fungsi BroadcastRTP untuk melihat apakah ia diteruskan dengan benar.

```


* **Virtual Network (VNet):** Jika integrasi tingkat lanjut diperlukan (menguji pertukaran ICE antar *peer* lokal), gunakan sub-package `vnet` dari Pion untuk mensimulasikan jaringan NAT/Router di dalam memori tanpa menyentuh *port* OS yang sebenarnya.

### 2.3. Mencegah Deadlock di Test

Ketika menguji fungsi berbasis *Go Channels*, gunakan `time.After` (timeout) pada `select` *statement* agar *test runner* tidak menggantung (hang) selamanya jika channel gagal mengirim atau menerima pesan.

---

## 3. Strategi Pengujian Egress Worker (Node.js)

Egress Worker berinteraksi langsung dengan Redis dan *binary* OS (FFmpeg). Isolasi adalah kunci agar *test* berjalan cepat dan deterministik.

### 3.1. Mocking Redis

Jangan gunakan *server* Redis sungguhan untuk *Unit Test*. Gunakan library `ioredis-mock`.

* **Skenario:** Menguji apakah Egress Worker berhasil melakukan *subscribe* ke *channel* `egress_commands` dan merespons JSON yang tepat tanpa menyentuh jaringan.

### 3.2. Mocking FFmpeg (Child Process)

Menjalankan FFmpeg akan menghabiskan CPU dan membuat *test* berjalan sangat lambat.

* **Teknik:** Lakukan *mocking* pada modul bawaan Node.js yaitu `child_process.spawn`.
* **Skenario:** Validasi apakah argumen *command-line* FFmpeg (seperti resolusi, *codec*, dan URL RTMP) disusun dengan benar oleh logika *builder* kamu, lalu simulasikan *event* `.on('close')` untuk memicu alur penyelesaian Egress.

---

## 4. Strategi Pengujian Frontend (Vue/React)

Frontend berurusan dengan manipulasi UI statis dan API *browser* yang kompleks.

### 4.1. Mocking Browser API

* API seperti `navigator.mediaDevices.getUserMedia` dan `navigator.mediaDevices.getDisplayMedia` harus diganti menggunakan fungsi *stub* (`jest.fn()` atau `vi.fn()`) yang mengembalikan *Promise* berisi objek tiruan `MediaStream`.
* Ini memastikan pengujian UI (seperti kemunculan kotak video partisipan baru) bisa dilakukan tanpa *browser* pengetes benar-benar meminta akses kamera PC kamu.

### 4.2. State Management Testing

Uji perubahan *state* secara sinkron. Misalnya: Saat *message* WebSocket bertipe `NEW_TRACK` dengan *kind* `screen` diterima, pastikan komponen *grid video* berubah memprioritaskan penempatan elemen tersebut ke tengah (*Stage Mode*).

---

## 5. Integration & End-to-End (E2E) Testing

Setelah *Unit Test* memiliki tingkat cakupan (*coverage*) minimal 85%, *Integration Test* dijalankan secara otomatis di lingkungan CI/CD.

* **Tools:** Gunakan **Testcontainers** (untuk Golang) untuk memutar *container* Redis asli dan *container* Egress secara terprogram dari dalam kode *test*.
* **Alur E2E Ringan:**
1. *Test* menjalankan Redis *container*.
2. Golang SFU melakukan inisialisasi.
3. Mengirim *mock* JWT dan SDP Offer ke *endpoint* Signaling.
4. Memvalidasi bahwa server SFU merespons dengan SDP Answer yang valid.