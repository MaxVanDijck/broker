---
title: Agent
weight: 4
---

The broker agent (`broker-agent`) is a single Go binary that runs on every provisioned node. It handles job execution, log streaming, SSH access, health reporting, and self-termination.

## Modes

The agent operates in two modes depending on where it runs:

### Host mode

Runs on the bare VM. Responsible for Docker container management:

- Pulling images
- Starting/stopping containers with GPU passthrough
- Managing network modes (host/bridge)
- Volume mounting

```bash
broker-agent --mode host --server ws://broker.example.com --cluster my-cluster
```

### Run mode

Runs inside a Docker container as the entrypoint. Responsible for job execution:

- Running setup and run scripts via PTY
- Streaming logs back to the server
- Providing SSH access into the container

```bash
broker-agent --mode run --server ws://broker.example.com --cluster my-cluster
```

## Connection model

The agent connects **outbound** to the server via WebSocket:

```
Agent ---> ws://server:8080/agent/v1/connect ---> Server
```

This means:

- Nodes do not need public IPs
- No inbound firewall rules required on the node
- Works through NATs and corporate proxies
- The server never initiates connections to agents

If the connection drops, the agent reconnects with exponential backoff (1s, 2s, 4s, ... up to 30s).

## Built-in SSH server

The agent includes a Go SSH server ([gliderlabs/ssh](https://github.com/gliderlabs/ssh)) on port 2222:

- Public key authentication
- PTY sessions with window resize handling
- Non-PTY exec (VS Code Remote SSH probing)
- Local and reverse port forwarding
- `direct-tcpip` channel handler

Connect via the CLI:

```bash
broker ssh my-cluster
```

Or configure `~/.ssh/config` for VS Code:

```
Host broker-*
  ProxyCommand broker ssh --stdio %h
  User root
  StrictHostKeyChecking no
```

## Heartbeats

The agent sends periodic heartbeats to the server containing:

- CPU and memory utilization
- Disk usage
- GPU metrics (utilization, memory, temperature) per GPU
- List of running job IDs

Heartbeat data is persisted to the analytics store (chdb or ClickHouse) for monitoring and cost tracking.

## Dead man's switch

The agent includes a watchdog that self-terminates the cloud instance if the server becomes unreachable. This prevents orphaned GPU instances from burning money.

**How it works:**

1. The watchdog is **armed** after the first successful registration with the server
2. Every message received from the server resets the timeout
3. If no server contact for the configured duration (default: 30 minutes), the watchdog triggers
4. On trigger, the agent auto-detects its cloud environment via instance metadata and calls the appropriate termination API

**Cloud detection and termination:**

| Cloud | Detection | Termination method |
|---|---|---|
| AWS | EC2 instance metadata (IMDSv2) | `aws ec2 terminate-instances` |
| GCP | GCE metadata server | `gcloud compute instances delete` |
| Azure | Azure IMDS | `shutdown -h now` (fallback) |
| Unknown | None respond | `shutdown -h now` |

Disable the watchdog for local development:

```bash
broker-agent --self-terminate-after 0 --server ws://localhost:8080 --cluster dev
```

## Configuration

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--server` | `BROKER_SERVER_URL` | | Server WebSocket URL |
| `--token` | `BROKER_TOKEN` | | Authentication token |
| `--cluster` | `BROKER_CLUSTER` | | Cluster name |
| `--node-id` | `BROKER_NODE_ID` | auto | Node identifier |
| `--ssh-port` | | `2222` | SSH server port |
| `--mode` | | `run` | `host` or `run` |
| `--self-terminate-after` | | `30m` | Terminate node if server unreachable for this duration (0 to disable) |
