# Review — service-monitor-master-go

## Ringkasan
Backend Go untuk MonitorHub: master sebagai server pusat, agent sebagai agen di mesin yang dimonitor.

## Cara Master-Agent Berkomunikasi
- **HTTP REST**: agent POST sync status service + hardware info ke master tiap interval check, dengan force-sync tiap 60 detik. Juga jadi jalur fallback pengiriman alert kalau WebSocket putus.
- **WebSocket persistent** ke `/api/v1/ws/agent`: channel real-time dua arah — master kirim perintah (action, mis. restart service), agent kirim alert saat status service berubah.
- **Auth**: header `X-Agent-ID` + `X-Secret-Key` di setiap request (HTTP maupun WS handshake). Master mencocokkan ke tabel institutions; perbandingan constant-time, fallback bcrypt kalau secret tersimpan sebagai hash.

## Status Temuan Lama (review 2026-10-03, diverifikasi ulang 2026-10-03)

| Prioritas | Temuan | Lokasi | Status |
|-----------|--------|--------|--------|
| Sedang | `websocket.Upgrader.CheckOrigin` selalu return true — handshake WS diterima dari origin mana pun. Auth header tetap dicek, tapi di deployment publik sebaiknya origin dibatasi. | `internal/api/handlers/ws.go:26` | ❌ BELUM DI-FIX |
| Sedang | Secret key agent boleh tersimpan plaintext di DB (auth coba plaintext dulu, baru bcrypt). Kalau DB bocor, semua credential agent ikut bocor. | `internal/api/middleware/auth.go:66-78` | ❌ BELUM DI-FIX |
| Perlu verifikasi | Config master tidak punya pengaturan TLS — asumsi TLS di-terminasi di reverse proxy. Pastikan agent tidak pernah konek via http/ws polos, karena secret key dikirim di setiap request. | config | ⚠️ MASIH BERLAKU |

## Temuan Baru (review ulang 2026-10-03)

| Prioritas | Temuan | Lokasi | Effort |
|-----------|--------|--------|--------|
| Tinggi | **Telegram bot token disimpan PLAINTEXT di DB.** Tidak ada kode enkripsi/dekripsi sama sekali di codebase (`grep encrypt` = 0 hasil). Token tersimpan di field `telegram_bot_token` (models.go:20,88) baik untuk setting global maupun per-institution. Kalau DB bocor, attacker dapat mengambil alih bot Telegram dan menerima semua alert. | `internal/models/models.go`, `internal/repository/mongo_repo.go` | Sedang (tambah enkripsi AES-256 di layer repository + migrasi data existing) |
| Sedang | **Tidak ada mekanisme migrasi DB yang jelas.** Tidak ada `AutoMigrate`, file migrasi, atau `ensureIndex` di codebase. Skema kemungkinan dibuat manual/implicit — berisiko drift antara environment. | `internal/database/` (hanya `seeder.go`) | Sedang (tambah migrasi versioned atau dokumentasikan setup) |
| Rendah | **Tidak ada dokumentasi API** (OpenAPI/Swagger). Menyulitkan integrasi frontend/mobile dan audit endpoint. | — | Sedang (generate dari code atau tulis manual) |

## Hal Positif (sudah bagus)
- ✅ Rate limiting: global 100 req/menit/IP + khusus login 5/menit (anti brute-force)
- ✅ IP blacklist otomatis untuk path mencurigakan + tarpit 1 detik untuk 404
- ✅ Security headers lengkap (CSP, HSTS, X-Frame-Options, dll)
- ✅ JWT secret wajib di-set (fatal kalau kosong), expiry 24 jam
- ✅ Health check dengan DB ping (`/health`)
- ✅ Query GORM pakai parameterized (`?`), query Mongo pakai `bson.M` dengan value langsung — aman dari SQL/NoSQL injection
- ✅ Tidak ada CORS wildcard yang berbahaya

## Rekomendasi
1. **WAJIB sebelum production**: enkripsi Telegram bot token (AES-256-GCM) + migrasi secret agent ke bcrypt-only. Ini dua-duanya soal credential plaintext di DB.
2. Batasi `CheckOrigin` ke origin yang dikenal untuk deployment publik (effort kecil: tambah config `ALLOWED_ORIGINS`).
3. Dokumentasikan requirement TLS di reverse proxy; pertimbangkan validasi skema URL di agent (tolak `http://`/`ws://`).
4. Tambah migrasi DB versioned sebelum tim bertambah besar.
5. Siapkan strategi backup/restore DB dan monitoring untuk master itu sendiri (who watches the watcher).
