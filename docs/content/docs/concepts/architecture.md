---
title: Architecture
weight: 1
---

broker uses a hub-and-spoke architecture with three components, two protocols, and two storage engines.

## Components

### Server

The control plane. A single Go binary serving everything on one HTTP port:

- **ConnectRPC API** -- handles CLI and SDK requests. Supports gRPC, gRPC-web, and plain HTTP/JSON simultaneously from the same handler.
- **WebSocket tunnel** -- accepts agent connections at `/agent/v1/connect`. Agents connect outbound; the server never initiates connections to nodes.
- **Dashboard** -- embedded React SPA served on the same port. Real-time SSE updates, metrics charts (CPU, memory, GPU), node detail pages, and one-click VS Code access. No separate frontend deployment.
- **Health endpoint** -- `/healthz` for load balancer probes, `/readyz` for full readiness checks.

For local development, the server auto-starts as a background process on first CLI use. No separate `broker-server` process is needed.

### CLI (`broker`)

A single static Go binary. Communicates with the server via ConnectRPC (HTTP POST with protobuf). Starts in under 50ms.

On first use, the CLI:
1. Auto-starts the server if not already running
2. Auto-installs SSH config so `*.broker` hostnames work immediately

The CLI provides `broker ssh` which tunnels SSH through the server via WebSocket, enabling `ssh <cluster>.broker` and VS Code Remote SSH without any manual SSH config setup.

### Agent (`broker-agent`)

A single Go binary deployed to every provisioned node. Runs in two modes:

- **Host mode** -- runs on the bare VM. Manages Docker containers, GPU allocation, image pulls, volume mounts.
- **Run mode** -- runs inside a Docker container as the entrypoint. Executes user commands, streams logs.

The agent includes:

| Component | Role |
|---|---|
| WebSocket tunnel | Outbound connection to server. Protobuf envelope framing. Exponential backoff reconnection. |
| Executor | PTY-based command runner. Setup + run scripts. Environment injection. Graceful cancellation. |
| SSH server | Built-in Go SSH server (gliderlabs/ssh). Public key auth. PTY sessions. Port forwarding. VS Code compatible. |
| Docker manager | Image pull, container run/stop/inspect. GPU passthrough. Host/bridge networking. |
| Heartbeat | Periodic node metrics (CPU, memory, GPU utilization). Reports running job IDs. Persisted to analytics store. |
| Watchdog | Dead man's switch. Self-terminates the cloud instance if the server is unreachable for a configurable duration. |

## Provisioning

broker provisions cloud instances directly via cloud APIs. For AWS, this includes:

- EC2 instance creation with GPU-optimized instance types
- Deep Learning AMI selection for GPU instances (pre-installed NVIDIA drivers, CUDA, Docker)
- Security group and key pair management
- User data scripts to bootstrap the agent on launch

## Storage

broker uses two storage engines matched to their access patterns:

### State store (OLTP)

Handles cluster, job, and user state. Requires ACID transactions and point lookups.

| Mode | Backend |
|---|---|
| Local/dev | SQLite (`~/.broker/broker.db`, WAL mode) |
| Production | PostgreSQL |

### Analytics store (OLAP)

Handles logs, metrics, and cost tracking. Append-only writes with time-range queries.

| Mode | Backend |
|---|---|
| Local/dev (default) | SQLite (`~/.broker/broker.db`) |
| Local with ClickHouse SQL | chdb (embedded ClickHouse, build with `-tags chdb`) |
| Production | ClickHouse |

Metrics persist locally by default in SQLite. The dashboard displays CPU, memory, and GPU utilization charts from this data. The `chdb` and ClickHouse backends use identical ClickHouse SQL -- queries written for one work on the other without modification.

## Protocols

### CLI <-> Server: ConnectRPC

The API is defined in `proto/broker.proto` and served via [ConnectRPC](https://connectrpc.com). This means:

- The CLI uses the **gRPC** wire protocol (binary, fast)
- The browser dashboard uses **gRPC-web** (same protobuf, works in browsers)
- `curl` uses the **Connect** protocol (plain HTTP POST with JSON)

All three are served by the same handler on the same port. No gateway needed.

```bash
curl -X POST http://localhost:8080/broker.v1.BrokerService/Status \
  -H 'Content-Type: application/json' \
  -d '{}'
```

### Agent <-> Server: WebSocket + Protobuf

The agent connects outbound to `ws://<server>/agent/v1/connect`. All messages are protobuf-encoded `Envelope` messages sent as WebSocket binary frames.

This design means:

- Nodes do not need public IPs
- No inbound firewall rules required
- Works through NATs, corporate proxies, and load balancers
- The server never initiates connections to agents

Message types:

| Direction | Message | Purpose |
|---|---|---|
| Agent -> Server | `Register` | Initial handshake with node info and auth token |
| Server -> Agent | `RegisterAck` | Accept/reject with heartbeat interval |
| Agent -> Server | `Heartbeat` | Periodic metrics and running job list |
| Agent -> Server | `LogBatch` | Batched log entries with timestamps |
| Agent -> Server | `JobUpdate` | Job state transitions |
| Server -> Agent | `SubmitJob` | Start a new job |
| Server -> Agent | `CancelJob` | Cancel a running job |
| Server -> Agent | `TerminateNode` | Shut down the agent and node |
