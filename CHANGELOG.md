# Changelog - Service Monitoring

> Dokumen ini mencatat semua perubahan yang telah dilakukan pada project.
> Baca dokumen ini sebelum menambah fitur atau melakukan perubahan besar.

---

## [v1.2.8] - 2026-10-24 - Scheduled Restart: Display Name Tidak Lagi Dikirim ke Agent

---

### service-monitor-master-go

#### fix(schedule): `service_names` berisi display_name membuat separuh restart gagal
- **Files**: `internal/scheduler/resolve.go` (baru), `internal/scheduler/scheduler.go`, `internal/api/handlers/schedule.go`
- **Problem**: Baris schedule dapat berisi `name` **dan** `display_name` untuk service yang sama. Agent hanya me-lookup `name` (`serviceMap[serviceName]`), sehingga setiap entri display_name dijawab `success:false, "Service not found in agent configuration"`. Gejalanya persis seperti laporan: 4 service → 8 perintah, `restarted 4/8 services (4 failed)`.
- **Fix**:
  - `scheduler.ResolveServiceNames` memetakan entri (Name **atau** DisplayName, case-insensitive) ke Name kanonik dan membuang duplikat.
  - Dipakai di jalur terjadwal (`executeSchedule`) dan trigger manual (`TriggerSchedule`); entri yang tidak dikenal dilaporkan sebagai `service not found on agent config` alih-alih dikirim mentah.
  - `CreateSchedule`/`UpdateSchedule` menormalkan `service_names` ke Name kanonik dan menolak service yang tidak dikenal, sehingga data baru tidak lagi menyimpan display_name.
  - Bila katalog service kosong (agent belum pernah sync), entri diteruskan apa adanya agar schedule lama tidak ikut ditolak.

### MonitorHub

#### fix(schedule): hitungan aktif & form edit memahami display_name
- **Files**: `src/app/(protected)/scheduled-restart/page.tsx`
- **Problem**: Baris schedule lama menyimpan display_name, sehingga `service_names.includes(svc.name)` tidak pernah cocok — badge menampilkan `0/4 Active` dan form edit terbuka tanpa service terpilih.
- **Fix**: Helper `resolveScheduledServices` mencocokkan Name maupun DisplayName (case-insensitive, tanpa duplikat) untuk badge dan form edit.

---

## [v1.2.7] - 2026-10-24 - Scheduled Restart: Alasan Kegagalan Tidak Lagi Hilang

---

### service-monitor-master-go

#### fix(scheduler): Balasan agent yang cepat tidak lagi hilang
- **Files**: `internal/scheduler/scheduler.go`, `internal/ws/registry.go`
- **Problem**: Channel penampung balasan dibuat unbuffered, sedangkan handler WS meneruskan `restart_result` dengan `select { case ch <- msg: default: }`. Bila agent membalas sebelum goroutine scheduler mencapai `select`, balasan dibuang dan run menunggu penuh 10 detik lalu tercatat `timeout` — padahal restart berhasil.
- **Fix**: Channel diberi buffer 1 di `executeSchedule` dan `Registry.RequestRestart`, sehingga forward non-blocking selalu berhasil.

#### fix(scheduler): Run yang gagal kini menyimpan alasan dari agent
- **Files**: `internal/scheduler/scheduler.go`, `internal/api/handlers/schedule.go`
- **Problem**: `ResultMessage` hanya berisi `restarted 0/2 services (2 failed)`. Pesan agent (mis. `SECURITY ALERT: Command injection detected!`) dibuang, sehingga operator tidak bisa membedakan penolakan agent, agent offline, dan timeout.
- **Fix**: Alasan per service ditambahkan ke `ResultMessage` pada jalur terjadwal maupun trigger manual, sehingga riwayat run di MonitorHub menampilkan penyebab sebenarnya.

---

## [v1.2.6] - 2026-10-24 - Daily Digest Reliability & Batch Alert Edit-in-Place

---

### service-monitor-master-go

#### fix(digest): Laporan harian tidak lagi terlewat karena penjadwalan exact-minute
- **Files**: cmd/server/main.go, internal/digest/digest.go, internal/api/handlers/setting.go
- **Problem**: Worker membandingkan `now.Format("15:04") == daily_digest_time`. Jika master restart, tick terlewat, atau jam tersimpan tidak persis `HH:MM` (mis. `8:00`), laporan hari itu dilewati tanpa log sama sekali.
- **Fix**: Menambahkan `digest.ShouldSendDigest` + `digest.ParseDigestTime` dengan catch-up window 15 menit: laporan dikirim bila jam target sudah lewat (≤15 menit) dan belum terkirim hari ini. Pengiriman yang gagal tidak menandai hari sebagai terkirim sehingga dicoba lagi pada tick berikutnya. Format jam divalidasi `HH:MM` saat simpan settings.

#### fix(digest): `SendDailyDigest` tidak lagi "sukses" diam-diam tanpa tujuan
- **Files**: internal/digest/digest.go, internal/api/handlers/setting.go
- **Problem**: Bila bot/chat global kosong dan tidak ada lembaga dengan bot custom, fungsi mengembalikan `nil` sehingga endpoint test melaporkan berhasil padahal tidak ada pesan terkirim.
- **Fix**: Menghitung pengiriman sukses dan mengembalikan error deskriptif bila nol; endpoint `POST /settings/test-daily-digest` juga menolak (400) saat daily digest dinonaktifkan.

#### fix(alert): Critical batch alert spam — edit pesan lama, bukan kirim ulang
- **Files**: internal/alert/manager.go
- **Problem**: Setiap gelombang DOWN baru (dan setiap DOWN berulang dari agent saat auto-restart gagal) memicu `sendMessage` baru, sehingga grup Telegram dibanjiri pesan `CRITICAL ALERT BATCH` yang isinya hampir sama.
- **Fix**: Service yang sudah tercantum di batch aktif tidak dikirim ulang (state di-refresh saja). Service baru yang ikut DOWN mengedit pesan batch yang sudah ada via `editMessageText`; kirim pesan baru hanya bila belum ada pesan batch atau seluruh edit gagal. Teks batch memakai builder bersama `buildBatchText` dengan jumlah service yang sedang DOWN.

#### feat(alert): State batch alert dipersistensikan agar bertahan saat master restart
- **Files**: internal/models/models.go, internal/repository/repository.go, internal/repository/sql_repo.go, internal/repository/mongo_repo.go, internal/alert/manager.go, cmd/server/main.go
- **Problem**: `BatchState` hanya di memori. Setelah master restart, DOWN berikutnya mengirim pesan `CRITICAL ALERT BATCH` baru, dan pesan lama tetap menggantung dalam kondisi "masih DOWN" tanpa pernah diedit.
- **Fix**: Menambahkan model `AlertBatchState` (tabel `alert_batch_states`; koleksi `alert_batch_states` di Mongo) berisi `message_id`, `chat_id`, `topic_id`, `is_global`, dan daftar service DOWN. Interface `repository.BatchStateStore` diimplementasikan SQL & Mongo; alert manager memuatnya kembali secara lazy (`hydrateBatch`) saat event pertama untuk lembaga itu datang, sehingga pesan lama diedit, bukan dikirim ulang. State dihapus saat semua service pulih atau institusi dihapus. Bot token tidak disimpan di tabel ini — target di-resolve ulang dari settings saat edit, jadi rotasi token tetap aman.

---


---

### service-monitor-master-go

#### fix(auth): Trim whitespace pada Agent ID & Secret Key, serta log Client IP saat auth gagal
- **Files**: internal/api/middleware/auth.go
- **Problem**: Header X-Agent-ID dan X-Secret-Key belum melewati strings.TrimSpace(), sehingga spasi/newline saat copy-paste ke config agen menyebabkan record not found atau mismatch di DB.
- **Fix**: Menggunakan strings.TrimSpace pada agentID, secretKey, dan storedSecret. Menambahkan pencatatan Client IP dan panjang karakter key pada log Master untuk diagnosa.

---

## [v1.2.4] - 2026-09-23 - WS Disconnect Race Condition & Institution Cleanup

---

### service-monitor-master-go

#### fix(ws): Perbaiki race condition saat unregister client koneksi WebSocket
- **Files**: internal/ws/registry.go, internal/api/handlers/ws.go, internal/ws/client.go
- **Problem**: Saat agent reconnect dengan cepat, koneksi lama yang sedang exit dapat memicu unregister dan mematikan koneksi baru yang baru saja masuk.
- **Fix**: Menambahkan UnregisterClient(agentID, client) dan IsCurrentClient agar koneksi lama tidak mematikan client baru. Menambahkan CloseWithReason(ClosePolicyViolation).

#### fix(institution): Bersihkan koneksi WebSocket dan cascade data saat institusi dihapus
- **Files**: internal/api/handlers/frontend.go, internal/repository/mongo_repo.go, internal/repository/sql_repo.go
- **Fix**: Memanggil CleanupInstitution sebelum delete di DB agar koneksi WS ditutup dengan rapi dan timer node offline dibatalkan. Menghapus cascading service, incident log, schedule, dan schedule runs.

#### fix(auth): Throttle log unauthorized agent
- **Files**: internal/api/middleware/auth.go
- **Fix**: Throttling pencatatan log unauthorized agent ke 1x per 5 menit per ID agar terminal tidak dibanjiri spam reconnect.

---

## [2026-04-24]  Session: Bug Fixes, UX Improvements, Config

---

### service-monitor-agent-go

#### fix(monitor): WS alert handler tidak menyimpan status ke DB
- **File**: `internal/monitor/monitor.go`
- **Problem**: Auto-restart hanya dipicu saat status BERUBAH (UP?DOWN). Jika service sudah DOWN dari awal atau tetap DOWN setelah restart gagal, auto-restart tidak pernah dipanggil lagi.
- **Fix**: Pisahkan logika auto-restart dari blok `if prevStatus != status`. Tambah `restartingServices map[string]bool` sebagai concurrent guard agar tidak spawn goroutine restart berulang setiap check interval.
- **Behavior baru**: Auto-restart dipicu setiap cycle DOWN. Jika restart sedang berjalan, cycle berikutnya skip dengan log `"Auto-restart already in progress"`. Setelah selesai, bisa trigger lagi.

#### fix(monitor): Auto-restart memblokir monitoring cycle
- **File**: `internal/monitor/monitor.go`
- **Problem**: Auto-restart berjalan synchronous di dalam goroutine worker yang sama dengan `wg.Done()`, menyebabkan `time.Sleep(10s)` memblokir seluruh monitoring cycle.
- **Fix**: Pindahkan auto-restart ke goroutine terpisah dengan context-aware sleep (`select { case <-time.After(10s): ... case <-ctx.Done(): ... }`).

---

### service-monitor-master-go

#### fix(ws): WS alert action tidak menyimpan status service ke DB
- **File**: `internal/api/handlers/ws.go`
- **Problem**: Handler `action == "alert"` hanya memanggil `AlertMgr.AddEvent()` tanpa memanggil `h.Repo.UpdateServiceStatus()`. Status DOWN tidak tersimpan ke DB, frontend tidak pernah tahu service down.
- **Fix**: Tambah pemanggilan `h.Repo.UpdateServiceStatus()` setelah `AlertMgr.AddEvent()` di WS alert handler.

#### fix(models): Tambah field RestartStatus ke Service model
- **Files**: `internal/models/models.go`, `internal/repository/repository.go`, `internal/repository/mongo_repo.go`, `internal/repository/sql_repo.go`
- **Change**: Tambah field `RestartStatus string` ke struct `Service` dengan tag `json:"restart_status"`. Tambah method `UpdateServiceRestartStatus` ke interface dan kedua implementasi repo.
- **Value contract**: `"restarting"` saat restart dimulai, `""` saat normal.

#### fix(timezone): Semua timestamp konsisten WIB (Asia/Jakarta)
- **Files**: `cmd/server/main.go`, `internal/alert/manager.go`, `internal/bot/bot.go`, `internal/api/middleware/security.go`, `internal/repository/sql_repo.go`, `internal/repository/mongo_repo.go`
- **Problem**: Sebagian timestamp pakai `time.Now()` (UTC), sebagian `time.Now().Local()`. Log yang dibuat jam 00:30 WIB tersimpan dengan tanggal kemarin di UTC.
- **Fix**: Set `time.Local = Asia/Jakarta` di awal `main()`. Ganti semua `time.Now()` ? `time.Now().Local()` di 5 file.
- **Note untuk Docker/Alpine**: Pastikan image punya `tzdata` (`apk add --no-cache tzdata`), atau `time.LoadLocation("Asia/Jakarta")` akan gagal.

#### feat(master): PATCH endpoint untuk toggle auto_restart service
- **Files**: `internal/repository/repository.go`, `internal/repository/mongo_repo.go`, `internal/repository/sql_repo.go`, `internal/api/handlers/frontend.go`, `cmd/server/main.go`
- **New endpoint**: `PATCH /api/v1/institutions/:id/services/:name`
- **Payload**: `{ "auto_restart": true/false }`
- **Response**: `{ "message": "auto_restart updated", "auto_restart": true/false }`
- **New method**: `UpdateServiceAutoRestart(ctx, instID, name, autoRestart)` di interface + kedua repo.

#### feat(master): Filter institution_id di endpoint audit logs
- **Files**: `internal/api/handlers/frontend.go`, `internal/repository/sql_repo.go`
- **Change**: Tambah query param `institution_id` di `GET /api/v1/audit-logs`. Mongo repo otomatis bekerja (filter langsung ke `$match`). SQL repo perlu case baru di filter handler.
- **Usage**: `GET /api/v1/audit-logs?institution_id={id}&page=1&limit=10`

---

### monitoring-service-hub (frontend)

#### fix(frontend): Hapus Node Security card yang berisi data palsu
- **File**: `src/app/(protected)/institutions/[id]/page.tsx`
- **Problem**: Card "Node Security" menampilkan `~14ms` latency dan `99.9%` uptime yang hardcoded  bukan data real.
- **Fix**: Hapus card sepenuhnya. Card "Node Hardware Analytics" tetap ada.

#### fix(frontend): Hapus kolom Category yang selalu "System"
- **File**: `src/app/(protected)/institutions/[id]/page.tsx`
- **Problem**: Kolom Category di services table selalu menampilkan "System" (hardcoded), tidak informatif.
- **Fix**: Hapus `<TableHead>` dan `<TableCell>` untuk kolom Category.

#### fix(frontend): Tambah konfirmasi dialog sebelum restart service
- **File**: `src/app/(protected)/institutions/[id]/page.tsx`
- **Change**: Restart button sekarang membuka `AlertDialog` konfirmasi terlebih dahulu. User harus klik "Restart" di dialog untuk melanjutkan. Klik "Batal" membatalkan aksi.
- **State baru**: `confirmRestart: string | null`  menyimpan nama service yang akan di-restart.

#### fix(frontend): Ganti live clock 1s interval dengan last-updated timestamp
- **File**: `src/app/(protected)/dashboard/page.tsx`
- **Problem**: `setInterval(..., 1000)` untuk live clock menyebabkan re-render setiap detik.
- **Fix**: Hapus interval, ganti dengan `formatDistanceToNow(summaryQuery.dataUpdatedAt)`  menampilkan "Updated X ago".

#### feat(frontend): Metric cards Total Institutions & Online Nodes bisa di-klik
- **File**: `src/app/(protected)/dashboard/page.tsx`
- **Change**: Bungkus dua metric card dengan `<Link href="/institutions">`. Card "Total Services" dan "Offline Alerts" tidak diberi link (tidak ada halaman tujuan yang relevan).

#### fix(frontend): Warna status pakai CSS token, bukan hardcoded
- **Files**: `src/app/globals.css`, `src/app/(protected)/institutions/[id]/page.tsx`, `src/app/(protected)/institutions/page.tsx`, `src/app/(protected)/dashboard/page.tsx`
- **Change**: Tambah CSS custom properties di `globals.css`:
  - `--status-up` (emerald)
  - `--status-down` (red-orange, sama dengan `--destructive`)
  - `--status-recovering` (amber)
  - `--status-offline` (slate)
  - Masing-masing punya nilai light dan dark mode.
- **Usage**: `text-[color:var(--status-up)]`, `bg-[color:var(--status-up)]/20`

#### fix(frontend): Hapus Header.tsx legacy (tidak dipakai)
- **File**: `src/components/layout/Header.tsx`  **DIHAPUS**
- **Note**: Header sekarang inline di `src/app/(protected)/layout.tsx`. File `Header.tsx` tidak diimport di mana pun.

#### feat(frontend): Toggle auto_restart via Switch UI di services table
- **File**: `src/app/(protected)/institutions/[id]/page.tsx`
- **Change**: Badge "Autonomous" diganti dengan `<Switch>` yang bisa di-toggle. Toggle memanggil `PATCH /api/v1/institutions/:id/services/:name` dengan payload `{ auto_restart: true/false }`.
- **Disabled**: Switch di-disable saat agent offline.

#### feat(frontend): Halaman /audit-logs tersendiri
- **Files**: `src/app/(protected)/audit-logs/page.tsx` (baru), `src/app/(protected)/settings/page.tsx`, `src/app/(protected)/layout.tsx`
- **Change**: Audit logs dipindahkan dari Settings tab ke halaman tersendiri `/audit-logs`.
- **Fitur baru**:
  - Filter per institusi (dropdown)
  - Export CSV (current page data)
  - Filter action, status, date range (sama seperti sebelumnya)
- **Settings tab**: Diganti dengan link ke `/audit-logs`.
- **Sidebar**: Tambah nav item "Audit Logs" dengan icon `ActivitySquare`.

#### fix(frontend): Session expired dialog, tidak auto-logout
- **File**: `src/components/providers/SessionProvider.tsx`
- **Problem**: Ketika session timeout habis, `handleLogout()` langsung dipanggil  `removeToken()` + redirect ke `/login` tanpa interaksi user.
- **Fix**: Pisahkan `handleExpired()` dari `handleLogout()`. Saat timeout habis, tampilkan dialog "Session Expired" yang menunggu user klik "Login Again". Tidak ada auto-redirect.
- **Behavior baru**:
  - Menit ke-(timeout-2): Dialog warning muncul ? "Stay Logged In" atau "Logout Now"
  - Menit ke-timeout: Dialog "Session Expired" muncul ? user harus klik "Login Again"
  - Dialog expired tidak bisa di-dismiss dengan klik di luar
- **State baru**: `isExpiredOpen: boolean`

#### docs: Tambah .env.example
- **File**: `.env.example` (baru)
- **Isi**: Dokumentasi semua env vars yang dipakai beserta keterangan dan default value.
- **Env vars**:
  - `NEXT_PUBLIC_API_URL`  URL backend API (wajib)
  - `NEXT_PUBLIC_TURNSTILE_SITE_KEY`  Cloudflare Turnstile site key (wajib)
  - `NEXT_PUBLIC_SESSION_TIMEOUT`  Durasi session timeout dalam menit (default: 30)
  - `NEXT_PUBLIC_TIMEZONE`  Timezone display (default: Asia/Jakarta)
- **Note**: Untuk Vercel, set via Dashboard ? Settings ? Environment Variables.

---

## Arsitektur & Catatan Penting

### Dual Database Support
Master mendukung dua database driver: **MongoDB** dan **SQL (PostgreSQL/MySQL)**. Setiap perubahan model atau query **wajib** diimplementasikan di kedua file:
- `internal/repository/mongo_repo.go`
- `internal/repository/sql_repo.go`

Dan didaftarkan di interface:
- `internal/repository/repository.go`

### WebSocket Flow (Agent ? Master)
```
Agent ? Master (WS):
  action: "sync"    ? BulkUpsertServices + UpdateInstitutionPing
  action: "alert"   ? UpdateServiceStatus + AlertMgr.AddEvent

Master ? Agent (WS):
  action: "restart" ? Agent eksekusi restart command, kirim balik restart_result

Agent ? Master (WS):
  action: "restart_result" ? Diteruskan ke pending request channel (frontend menunggu)
```

### Auto-Restart Flow (Agent)
```
checkAll() setiap CheckIntervalSeconds detik:
  1. checkService() ? status UP/DOWN
  2. Jika status berubah ? kirim alert ke master
  3. Jika DOWN + auto_restart + tidak sedang restart:
     ? spawn goroutine restart (non-blocking)
     ? set restartingServices[name] = true
     ? sleep 10s (context-aware)
     ? eksekusi RestartCommand
     ? delete restartingServices[name]
```

### Frontend Auth Flow
```
Login ? JWT token ? disimpan di localStorage (Zustand persist)
Protected routes ? cek token di useEffect ? redirect /login jika tidak ada
Session timeout ? warning dialog (2 menit sebelum) ? expired dialog (saat habis)
Expired dialog ? user klik "Login Again" ? removeToken() + redirect /login
```

### Env Vars Master (service-monitor-master-go)
Lihat `service-monitor-master-go/.env.example` untuk daftar lengkap. Variabel wajib:
- `BOT_TOKEN`  Telegram bot token
- `JWT_SECRET`  Secret untuk signing JWT
- `DB_DRIVER`  `mongo`, `postgres`, atau `mysql`

### Env Vars Frontend (monitoring-service-hub)
Lihat `monitoring-service-hub/.env.example` untuk daftar lengkap. Variabel wajib:
- `NEXT_PUBLIC_API_URL`  URL backend API
- `NEXT_PUBLIC_TURNSTILE_SITE_KEY`  Cloudflare Turnstile site key