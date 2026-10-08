# Deploy service-monitor-master-go dengan Docker

## 1. Siapkan `.env`

```bash
cp .env.example .env
nano .env
```

Wajib diisi:
- `DB_DRIVER=mongo`
- `DB_MONGO_URI=mongodb://...` (MongoDB yang sudah ada)
- `BOT_TOKEN` (token bot Telegram)
- `JWT_SECRET` (random, 32+ karakter)
- `CAPTCHA_SECRET` (random, 32+ karakter) — atau `CAPTCHA_PROVIDER=none`
- **`MASTER_ENCRYPTION_KEY`** (hex 64 karakter / 32 byte — generate: `openssl rand -hex 32`).
  Ini LEBIH AMAN daripada file `data/.encryption_key` karena key ikut backup
  bersama `.env` dan tidak hilang kalau volume dihapus.

## 2. Build & jalan

Image Docker dibangun **otomatis oleh GitHub Actions** tiap push ke branch
`security-fixes` dan dipush ke GHCR:
`ghcr.io/anggialansori404/service-monitor-master:latest`.
Repo private → image private, jadi server perlu login sekali:

```bash
# 1. Bikin Personal Access Token (sekali saja):
#    github.com → Settings → Developer settings → Personal access tokens
#    → Tokens (classic) → Generate → centang read:packages → copy token-nya

# 2. Di server data center:
echo "<PAT>" | docker login ghcr.io -u anggialansori404 --password-stdin

# 3. Siapkan folder deploy (cukup 3 file, TIDAK perlu clone repo):
mkdir -p ~/svcmon && cd ~/svcmon
# - salin docker-compose.yml, .env (dari .env.example), buat folder logs/ data/

# 4. Jalan:
docker compose pull
docker compose up -d
docker compose logs -f
curl http://127.0.0.1:8080/health   # harus 200
```

> Alternatif tanpa GHCR: clone repo di server (pakai deploy key),
> lalu ganti `image:` jadi `build: .` di docker-compose.yml
> dan jalankan `docker compose up -d --build`.

## 3. Pasang nginx (di VPS)

File: `nginx/svcmon.mangdev.net.conf` — **file baru**, jangan edit conf lain.

```bash
# di VPS
sudo cp svcmon.mangdev.net.conf /etc/nginx/conf.d/
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d svcmon.mangdev.net
```

Ganti `svcmon.mangdev.net` dengan domain pilihan bila beda
(jangan lupa sesuaikan `server_name` di conf).

## 4. Cek

- `https://svcmon.mangdev.net/health` → 200
- `https://svcmon.mangdev.net/ready` → 200 (butuh DB konek)
- Agent di server klien arahkan ke `https://svcmon.mangdev.net`
  (endpoint `/api/v1/agent/sync` + websocket `/api/v1/ws/agent`)

## Catatan

- Container hanya bind ke `127.0.0.1:8080` — publik hanya lewat nginx.
- Log aplikasi: `./logs/` (daily rotating, retention diatur via env
  `AUDIT_LOG_RETENTION_DAYS` dll).
- Update: `git pull && docker compose up -d --build`
