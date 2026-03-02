# Service Monitor Master

A robust Go-based monitoring service designed to track the health of multiple institutions and their services. Features real-time updates via WebSockets, Telegram notifications, and advanced security protections.

## 🚀 Features

- **Multi-Institution Monitoring**: Track multiple nodes and services from a single dashboard.
- **Real-time Metrics**: Capture CPU, RAM, and OS platform information from agents.
- **Telegram Integration**: Get instant alerts for service status changes.
- **WebSocket Support**: Live status updates for the frontend dashboard.
- **Dual Database Support**: Works with MongoDB Atlas or SQL (PostgreSQL/MySQL).
- **Service Management Script**: Easy `service.sh` for background execution and daily logging.

## 🛡️ Security Implementation

The system is equipped with advanced security measures to protect against automated attacks:

- **Automated IP Blacklist**: Automatically blocks IPs attempting to access suspicious paths (e.g., `.env`, `wp-admin`, `.php`).
- **IP Blacklist Middleware**: Rejects requests from blacklisted IPs with a 403 Forbidden response.
- **Security Headers**: Standard protection via XSS-Protection, CSP, HSTS, and frame options.
- **Rate Limiting**: 
  - Global: 100 requests/minute per IP.
  - Login: 5 attempts/minute per IP for brute-force protection.
- **Suspicious Path Logger**: Audit logs for all security-related events.
- **Tarpit Mechanism**: 1-second delay for all 404 responses to slow down scanners.

## 📋 Prerequisites

- Go 1.25.5 or later
- MongoDB Atlas or a SQL database (MySQL/PostgreSQL)
- Telegram Bot Token (optional, for alerts)

## ⚙️ Configuration

Copy `.env.example` to `.env` and fill in your credentials:

```bash
cp .env.example .env
```

| Variable | Description |
|----------|-------------|
| `DB_DRIVER` | `postgres`, `mysql`, or `mongo` |
| `DB_MONGO_URI` | Connection string for MongoDB |
| `BOT_TOKEN` | Telegram Bot API Token |
| `JWT_SECRET` | Secret key for login tokens |
| `PORT` | Server port (default 8080) |

## 🛠️ Installation & Running

1. **Clone the repository**:
   ```bash
   git clone <repository-url>
   cd monitoring-master
   ```

2. **Download dependencies**:
   ```bash
   go mod download
   ```

3. **Build the project**:
   ```bash
   go build -o monitoring-server-linux-amd64.bin cmd/server/main.go
   ```

4. **Run using the service script**:
   ```bash
   chmod +x service.sh
   ./service.sh start
   ```

## 📂 Project Structure

- `cmd/server/`: Main application entry point.
- `internal/api/`: Handlers and security middleware.
- `internal/models/`: Database models and schemas.
- `internal/repository/`: Data access layer for SQL and MongoDB.
- `internal/bot/`: Telegram bot implementation.
- `internal/ws/`: WebSocket registry and management.

## 📄 License

This project is proprietary. All rights reserved.
