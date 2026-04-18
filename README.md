# kingshot-redeemer

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Self-hostable service that automatically redeems Kingshot gift codes for a list of player accounts. Runs on a configurable interval, skips already-redeemed codes, and persists results to a local SQLite database.

## How it works

1. On each poll interval, fetches active gift codes from the Kingshot API
2. For each code, filters out players who already redeemed it
3. Sends bulk redeem requests and processes the streaming SSE response
4. Saves successful redemptions to SQLite
5. Marks expired codes so they are never retried
6. Failed (non-expired) redemptions are retried on the next tick

## Quick start

**With Docker (recommended):**

```bash
# 1. Create your player IDs file (one ID per line)
printf '12345678\n87654321\n' > players.txt

# 2. Create empty DB file (required for Docker volume mount)
touch redeemer.db

# 3. Start the service
docker compose up -d

# 4. Follow logs
docker compose logs -f
```

**Without Docker:**

```bash
go build -o ks-redeemer .
PLAYER_FILE=./players.txt ./ks-redeemer
```

## Configuration

All configuration is via environment variables. Set them in `docker-compose.yml` or export them before running the binary.

| Variable        | Default                                           | Description                               |
| --------------- | ------------------------------------------------- | ----------------------------------------- |
| `PLAYER_FILE`   | `./players.txt`                                   | Path to player IDs file (one ID per line) |
| `DB_PATH`       | `./redeemer.db`                                   | SQLite database path (auto-created)       |
| `POLL_INTERVAL` | `15m`                                             | Poll interval, e.g. `30s`, `10m`, `1h`   |
| `BATCH_SIZE`    | `3`                                               | Players per redeem request (max 100)      |
| `WORKERS`       | `5`                                               | Concurrent batch requests (max 20)        |
| `CODES_URL`     | `https://kingshot.net/api/gift-codes`             | Gift codes API endpoint                   |
| `REDEEM_URL`    | `https://kingshot.net/api/gift-codes/bulk-redeem` | Bulk redeem API endpoint                  |
| `HEALTH_URL`    | `https://kingshot.net/api/health`                 | Health check endpoint                     |

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
├── redeemer/             # Bulk redeem requests and SSE response parsing
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
