---
title: Configuration
weight: 3
---

## Server configuration

The server reads `~/.broker/config.yaml` on startup.

### Minimal (local development)

```yaml
api_server:
  http_port: 8080
```

No other configuration is needed for local development. The server uses SQLite for state and discards analytics data by default.

### Production

```yaml
api_server:
  http_port: 8080

state:
  backend: postgres
  dsn: postgres://user:pass@db.example.com:5432/broker

analytics:
  backend: clickhouse
  dsn: clickhouse://user:pass@ch.example.com:9000/broker
```

### Full reference

#### api_server

| Field | Default | Description |
|---|---|---|
| `http_port` | `8080` | Port for all traffic (API, agent tunnel, dashboard, healthcheck) |

#### state

Controls where cluster and job state is stored (OLTP).

| Field | Default | Description |
|---|---|---|
| `backend` | `sqlite` | `sqlite` or `postgres` |
| `dsn` | | PostgreSQL connection string (required when backend is `postgres`) |

#### analytics

Controls where logs, metrics, and cost data are stored (OLAP).

| Field | Default | Description |
|---|---|---|
| `backend` | `noop` | `noop`, `chdb`, or `clickhouse` |
| `dsn` | | ClickHouse connection string (required when backend is `clickhouse`). For `chdb`, this is the data directory path (defaults to `~/.broker/chdb`). |

The `noop` backend discards all analytics data. Use this when you don't need log persistence or metrics.

The `chdb` backend requires the `libchdb` native library and building with `-tags chdb`. It provides embedded ClickHouse -- same SQL, same compression, zero external dependencies.

## Data directory

```
~/.broker/
  config.yaml    # Server configuration
  broker.db      # SQLite state database (WAL mode)
  chdb/          # chdb analytics data (if using chdb backend)
```
