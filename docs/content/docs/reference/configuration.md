---
title: Configuration
weight: 3
---

## Server configuration

The server reads `~/.broker/config.yaml` on startup.

### Minimal (local development)

No configuration file is needed. The server uses SQLite for both state and analytics by default, and auto-starts on first CLI use.

```yaml
api_server:
  http_port: 8080
```

### Production

```yaml
api_server:
  http_port: 8080
  public_url: wss://broker.example.com  # URL agents use to connect back

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
| `public_url` | | URL agents use to connect back to the server (e.g. `wss://broker.example.com`). Required for cloud provisioning. |

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
| `backend` | `sqlite` | `sqlite`, `chdb`, or `clickhouse` |
| `dsn` | | Connection string. For `sqlite`, defaults to `~/.broker/broker.db`. For `chdb`, defaults to `~/.broker/chdb`. For `clickhouse`, a connection string is required. |

By default, metrics persist locally in SQLite. Heartbeat data (CPU, memory, GPU utilization) is stored and available in the dashboard.

The `chdb` backend requires the `libchdb` native library and building with `-tags chdb`. It provides embedded ClickHouse -- same SQL, same compression, zero external dependencies.

## Data directory

```
~/.broker/
  config.yaml    # Server configuration
  broker.db      # SQLite state + analytics database (WAL mode)
  server.log     # Auto-started server log output
  server.pid     # PID of auto-started server process
  ssh_config     # Auto-installed SSH config for *.broker hostnames
  chdb/          # chdb analytics data (if using chdb backend)
```
