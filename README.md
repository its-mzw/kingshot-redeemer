# kingshot-redeemer

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Self-hostable service that automatically redeems Kingshot gift codes for a list of player accounts. Runs on a configurable interval, skips already-redeemed codes, and persists results to a local SQLite database.

## How it works

1. On each poll interval, fetches active gift codes from the Kingshot API
2. For each code, filters out players who already redeemed it
3. Sends one redeem request per player (cookie-auth) and processes the JSON response
4. Saves successful redemptions to SQLite
5. Marks expired and already-redeemed codes so they are never retried
6. Failed (unknown error) redemptions are retried on the next tick

## Quick start

**With Docker (recommended):**

```bash
# 1. Create your player IDs file (one ID per line)
printf '12345678\n87654321\n' > players.txt

# 2. Create empty DB and skipping_codes files (required for Docker volume mounts)
touch redeemer.db skipping_codes.txt

# 3. Set your session token in docker-compose.yml (see Configuration below)

# 4. Start the service
docker compose up -d

# 5. Follow logs
docker compose logs -f
```

**Without Docker:**

```bash
go build -o ks-redeemer .
SESSION_TOKEN=<your-session-token> PLAYER_FILE=./players.txt ./ks-redeemer
```

## Configuration

All configuration is via environment variables. Set them in `docker-compose.yml` or export them before running the binary.

| Variable        | Default                                      | Description                                                     |
| --------------- | -------------------------------------------- | --------------------------------------------------------------- |
| `SESSION_TOKEN` | _(required)_                                 | Value of `__Secure-next-auth.session-token` cookie from browser |
| `PLAYER_FILE`   | `./players.txt`                              | Path to player IDs file (one ID per line)                       |
| `SKIPPING_FILE` | `./skipping_codes.txt`                       | Path to codes to skip (one code per line)                       |
| `DB_PATH`       | `./redeemer.db`                              | SQLite database path (auto-created)                             |
| `POLL_INTERVAL` | `15m`                                        | Poll interval, e.g. `30s`, `10m`, `1h`                          |
| `WORKERS`       | `5`                                          | Concurrent redeem requests per code (max 20)                    |
| `CODES_URL`     | `https://kingshot.net/api/gift-codes`        | Gift codes API endpoint                                         |
| `REDEEM_URL`    | `https://kingshot.net/api/gift-codes/redeem` | Redeem API endpoint                                             |
| `HEALTH_URL`    | `https://kingshot.net/api/health`            | Health check endpoint                                           |

### Getting your session token

1. Log in to [kingshot.net](https://kingshot.net) in your browser
2. Open DevTools → Application → Cookies
3. Copy the value of `__Secure-next-auth.session-token`
4. Set it as `SESSION_TOKEN` in your environment or `docker-compose.yml`

The token is tied to your login session. If it expires, the service will log auth errors — re-login and update the token.

## Player ID file

Plain text, one ID per line, blank lines ignored:

```
12345678
87654321
11223344
```

The file is re-read on every tick, so you can add or remove players without restarting the service.

## Inspecting redemptions

```bash
# All redemptions
sqlite3 redeemer.db "SELECT * FROM redemptions ORDER BY redeemed_at DESC;"

# Summary per code
sqlite3 redeemer.db "SELECT code, status, count(*) FROM redemptions GROUP BY code, status;"
```

## Running tests

```bash
go test ./...
```

## Project layout

```
├── main.go               # Entry point
├── config/               # Environment-based configuration
├── poller/               # API health checks and code fetching
├── redeemer/             # Per-player redeem requests and JSON response parsing
├── scheduler/            # Poll loop and orchestration
└── store/                # SQLite persistence
    └── migrations/       # SQL migration files (embedded in binary)
```

## Disclaimer

This tool is intended for **personal use only** — to redeem gift codes for your own accounts. Do not use it to exploit the Kingshot platform, abuse API rate limits. The authors take no responsibility for misuse.

## Acknowledgements

Thanks to the team at [kingshot.net](https://kingshot.net/contributors) for building the platform and providing a public API that makes tooling like this possible.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
