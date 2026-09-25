# hive-health-app

HIVE warehouse stock service: a Go HTTP API and a small HTML page on top of PostgreSQL.

## Run locally

```bash
docker compose up --build      # Postgres + HIVE with sample data on :8080
go test ./...                  # unit tests, no database needed
```

## Commands

| Command        | What it does                                                          |
|----------------|-----------------------------------------------------------------------|
| `hive`         | Runs the server. Applies migrations first unless `AUTO_MIGRATE=false`. |
| `hive migrate` | Applies migrations (and seeds if `SEED_SAMPLE_DATA=true`), then exits. For an init container or a Job. |

Migrations live in `migrations/` and are embedded in the binary. They run under a
Postgres advisory lock, so several replicas can start at the same time safely.

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `DATABASE_URL` | – | Full Postgres URL. Overrides the `DB_*` variables. |
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_NAME` | `localhost` / `5432` / `hive` / `hive` | |
| `DB_PASSWORD` | – | **Required** unless `DATABASE_URL` is set. |
| `DB_SSLMODE` | `prefer` | |
| `DB_MAX_OPEN_CONNS` / `DB_MAX_IDLE_CONNS` | `10` / `5` | |
| `DB_CONN_MAX_LIFETIME` | `30m` | |
| `DB_CONNECT_TIMEOUT` | `30s` | How long to wait for the database at startup. |
| `PORT` | `8080` | |
| `AUTO_MIGRATE` | `true` | |
| `SEED_SAMPLE_DATA` | `false` | Inserts sample products only when the table is empty. |
| `SHUTDOWN_TIMEOUT` | `15s` | Time to drain requests after SIGTERM. |

## Endpoints

| Method | Path | |
|---|---|---|
| GET | `/` | Stock table (HTML) |
| GET | `/api/stock` | All products |
| POST | `/api/stock` | Stock movement `{"product_id":1,"delta":-5,"note":"..."}`. 409 if stock would go negative. |
| GET | `/api/stock/{id}` | One product. Used by the mobile app, keep the contract. |
| GET | `/api/movements` | Last 100 movements |
| GET | `/api/reports` | Last 50 daily reports (filled by the external `report.sh` job) |
| GET | `/healthz` | Liveness: the process is up (does not check the database) |
| GET | `/readyz`, `/health` | Readiness: the database is reachable |
