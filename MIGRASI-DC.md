# Persiapan Mini PC (Data Center) — Migrasi api-monitoring.mangdev.net

Migrasi service-monitor master dari VPS lama (43.157.226.129) ke mini PC di DC.
Metode: Docker, image dari GHCR (dibangun otomatis oleh GitHub Actions).

---

## 1. OS & akses

- Ubuntu 22.04 / 24.04 LTS (server, tanpa GUI)
- Punya akses root/sudo via SSH
- IP publik dari data center

## 2. Install software

```bash
sudo apt update && sudo apt install -y nginx python3-certbot-nginx

# Docker (official repo)
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER   # lalu re-login
docker compose version          # pastikan plugin compose ada
```

## 3. Jaringan & DNS

- Firewall/NAT di DC: buka **80, 443** (dan 22 untuk SSH)
- **JANGAN pindah DNS dulu.** Turunkan TTL record `api-monitoring.mangdev.net`
  ke 300 detik minimal H-1 sebelum cutover, biar perpindahan cepat.

## 4. Siapkan folder deploy

```bash
mkdir -p ~/svcmon/{logs,data} && cd ~/svcmon
```

Isi folder `~/svcmon/`:

| File | Sumber |
|---|---|
| `docker-compose.yml` | dari repo `service-monitor-master-go` (branch `security-fixes`) |
| `.env` | **copy dari VPS lama** `/home/ubuntu/monitoring-master/.env` |
| `data/.encryption_key` | **copy dari VPS lama** `/home/ubuntu/monitoring-master/data/.encryption_key` |
| `logs/` | kosong, dibuat otomatis |

> ⚠️ **KRITIKAL: `data/.encryption_key` WAJIB dicopy dari VPS lama.**
> Tanpa file ini, semua secret terenkripsi di MongoDB tidak bisa dibaca lagi.
> Alternatif: isi `MASTER_ENCRYPTION_KEY` di `.env` dengan isi file tersebut,
> maka `data/` tidak diperlukan.

`.env` yang dicopy dari VPS lama sudah berisi: `DB_DRIVER=mongo`,
`DB_MONGO_URI` (Atlas), `BOT_TOKEN`, `JWT_SECRET`, `PORT=4001`, dll.
Tidak perlu ubah apa-apa kecuali mau ganti port.

## 5. Login GHCR (sekali saja)

Image Docker private di `ghcr.io`. Butuh Personal Access Token
(github.com → Settings → Developer settings → Tokens classic → `read:packages`):

```bash
echo "<PAT>" | docker login ghcr.io -u anggialansori404 --password-stdin
```

## 6. Jalankan master

```bash
cd ~/svcmon
docker compose pull
docker compose up -d
docker compose logs -f                 # cek tidak ada error
curl http://127.0.0.1:4001/health    # harus 200 (atau port sesuai .env)
```

## 7. nginx

Pakai conf dari repo: `nginx/svcmon.mangdev.net.conf` sebagai acuan,
tapi sesuaikan:

- `server_name api-monitoring.mangdev.net;`
- `proxy_pass http://127.0.0.1:4001;` (atau port sesuai `.env`)
- **Pertahankan dari conf lama**: security headers, websocket upgrade
  di `/api/v1/ws/agent`, `proxy_read_timeout 3600s`

```bash
sudo cp api-monitoring.mangdev.net.conf /etc/nginx/conf.d/
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d api-monitoring.mangdev.net
```

## 8. Cutover (saat VPS lama mau off)

1. Pastikan container di mini PC sehat (`/health` 200 via nginx)
2. Pindah DNS `api-monitoring.mangdev.net` → IP publik mini PC
3. Tunggu propagasi (dengan TTL 300, ~5-10 menit)
4. Test: `curl https://api-monitoring.mangdev.net/health`
5. Test login + cek agent-agent masih sync
6. Matikan service di VPS lama

## Rollback

Kalau ada masalah: kembalikan DNS ke IP VPS lama (selama VPS belum off).
Container di mini PC bisa tetap jalan untuk investigasi.
