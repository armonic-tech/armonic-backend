<div align="center">

# Armonic

**A self-hosted, real-time messaging platform - text channels and voice/WebRTC channels, on infrastructure you own.**

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org)
[![WebRTC](https://img.shields.io/badge/WebRTC-pion-333333?logo=webrtc&logoColor=white)](https://github.com/pion/webrtc)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)](./docker-compose.yml)

**Backend** - [Frontend →](https://github.com/armonic-tech/armonic-client)

</div>

---

## What is Armonic?

Armonic is an open-source, self-hostable alternative to Discord-style community servers. Run a single instance for your team, community, or friends and keep every message, account, and voice call on hardware you control - no third-party service, no data leaving your box.

- 💬 **Text channels** over WebSocket, with message history.
- 🎙️ **Voice channels** over WebRTC, relayed by a built-in SFU.
- 🔒 **Invite-only by design** - no public signup. The instance boots unclaimed, the operator claims it with an instance password, and every other account is created through single-use invite links.
- 🪶 **Single binary, single instance** - easy to deploy, easy to reason about.

This repository is the **backend** (Go). The **[Flutter client lives here](https://github.com/armonic-tech/armonic-client)**.

## Tech stack

| Layer | Technology |
| --- | --- |
| Language | Go 1.25 |
| Real-time signaling | WebSocket (`gorilla/websocket`) |
| Voice media | WebRTC SFU (`pion/webrtc`) |
| Persistence | PostgreSQL 17 (`pgx`) |
| Auth | Username/password + JWT (HS256, `bcrypt`) |
| Logging | Structured JSON (`log/slog`) |

## Quick start

### With Docker (recommended)

```bash
git clone https://github.com/armonic-tech/armonic-backend.git
cd armonic-backend
# Set a real app.password in config.json and JWT_SECRET in docker-compose.yml
docker compose up --build
```

The server comes up on `http://localhost:8080`. Point the [client](https://github.com/armonic-tech/armonic-client) at it, enter the instance password to claim the server, and register the admin account.

### From source

```bash
go build ./...
go test $(go list ./... | grep -v /repositories | grep -v /internal/server)   # unit tests
DATABASE_URL=postgres://armonic:armonic@localhost:5432/armonic?sslmode=disable go run .
```

## Configuration

Configuration is read from environment variables (a local `.env` file is auto-loaded) plus the instance-wide claim password in `config.json`.

| Variable | Default | Description |
| --- | --- | --- |
| `DATABASE_URL` | local dev DSN | Postgres connection string |
| `JWT_SECRET` | `change-me` | Signs auth JWTs - **override in production** |
| `PORT` | `8080` | HTTP listen port |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `LOG_FILE` | *(stdout only)* | Absolute path to also append JSON logs |
| `config.json` → `app.password` | - | Instance claim secret - **required to start** |

## How it works

The server always boots without an owner. On first run it creates a default server with a `general` text channel and a `General` voice channel, then waits to be **claimed**:

1. The operator submits the instance password → receives a single-use ticket.
2. The ticket is redeemed to create the admin account and take ownership.
3. The admin generates single-use invite links; each new member redeems one to create their account.

Text messages travel over a single WebSocket connection (one JSON envelope discriminated by `type`); voice traffic is negotiated over WebRTC and relayed to other participants by the SFU in `channel.VoiceChannel`.

See [`docs/ai/`](docs/ai/) for the architecture, HTTP/WebSocket API reference, and current known gaps.

## Project layout

```
config/           Environment + config.json loading
internal/
  models/         In-memory runtime graph (app, server, channel, user, message)
  repositories/   Postgres persistence, one *Repo per table
  handlers/       WS/HTTP handlers wiring persistence to real-time state
  transport/      ws (gorilla) and rtc (pion) adapters
  auth/           Signup/login, JWT
  claim/          Password-gated instance claiming
  server/         Composition root - wires everything, registers routes
migrations/       Raw SQL schema (applied on startup)
pkg/logger/       Structured slog setup
```

## Contributing

Issues and pull requests are welcome. Please run `go vet ./...` and the test suite before opening a PR.

## License

Armonic is licensed under the **GNU Affero General Public License v3.0** - see [LICENSE](LICENSE). If you run a modified version of Armonic as a network service, the AGPL requires you to make your source available to its users.
