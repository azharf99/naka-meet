# Database Design

## 1. Relational DB (PostgreSQL / MySQL) - Persistent Data
- **Table `users`**
  - `id` (UUID, PK)
  - `name` (Varchar)
  - `email` (Varchar, Unique)
  - `password_hash` (Varchar)
- **Table `rooms`**
  - `id` (UUID, PK)
  - `slug` (Varchar, Unique) 
  - `host_id` (UUID, FK -> users.id)
  - `created_at` (Timestamp)
- **Table `recordings`**
  - `id` (UUID, PK)
  - `room_id` (UUID, FK -> rooms.id)
  - `s3_url` (Varchar) - URL penyimpanan file hasil egress
  - `status` (Enum: 'processing', 'completed', 'failed')

## 2. Redis - Ephemeral State & Message Broker
- **Key-Value State:**
  - `room:{room_slug}:participants` (Hash) -> Menyimpan ID partisipan yang sedang aktif.
  - `session:{session_id}` (String) -> Penanda waktu habis untuk *graceful reconnect* WebSocket.
- **Pub/Sub Channels:**
  - `channel:egress_commands` -> Digunakan SFU untuk mengirim *payload* JSON ke *Egress Worker* (contoh: `{"action": "START_RTMP", "url": "..."}`).