# Business Rules

- **BR1 (Otoritas Host):** Hanya *user* yang berstatus sebagai pembuat ruangan (Host) yang memiliki izin untuk memicu *endpoint* perekaman (*recording*), dorongan *live streaming* RTMP, atau membungkam mikrofon peserta lain.
- **BR2 (Egress Lifecycle):** 
  - Jika tidak ada peserta (*publisher*) yang tersisa di dalam ruangan selama lebih dari 5 menit, proses *Egress Worker* (FFmpeg/Puppeteer) harus dihentikan secara otomatis untuk menghemat *resource* CPU.
  - Proses *shutdown* FFmpeg harus menggunakan sinyal `SIGINT` (bukan `SIGKILL`) agar metadata *file* MP4/FLV tertulis dengan benar.
- **BR3 (Kapasitas VPS Tunggal):** Untuk menjaga kestabilan *bandwidth egress* server, partisipan maksimal per ruangan dibatasi pada 50 koneksi aktif (*hard-limit* di lapisan Golang).
- **BR4 (Prioritas Rendering UI):** Ketika *metadata out-of-band* mendeteksi jenis *track* `screen` (presentasi layar) dari Pion, *frontend* harus otomatis mengubah *layout* video menjadi *Picture-in-Picture* atau *Stage Mode*, memprioritaskan layar presentasi di tengah.