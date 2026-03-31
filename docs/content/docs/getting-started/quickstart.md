---
title: Quickstart
weight: 2
---

This guide walks through starting the server, connecting an agent, and running your first job.

## Start the server

```bash
make build && ./bin/broker-server
```

The server starts on port 8080 and serves everything on a single port:

- Dashboard at `http://localhost:8080`
- ConnectRPC API for the CLI
- WebSocket endpoint for agent connections
- Health check at `/healthz`

## Connect an agent

On a node (or locally for testing):

```bash
broker-agent --server ws://localhost:8080 --cluster my-cluster
```

The agent will:

1. Connect to the server via WebSocket
2. Register with its node info (hostname, CPUs, GPUs)
3. Start the built-in SSH server on port 2222
4. Begin sending heartbeats
5. Arm the dead man's switch (auto-terminates the node if the server becomes unreachable)

For local testing, disable self-termination:

```bash
broker-agent --server ws://localhost:8080 --cluster my-cluster --self-terminate-after 0
```

## Launch a cluster

Create a task file `task.yaml`:

```yaml
name: hello-world
run: |
  echo "Hello from broker"
  nvidia-smi
resources:
  accelerators: A100:1
```

Launch it:

```bash
broker launch -c my-cluster task.yaml
```

Or launch with inline commands:

```bash
broker launch -c my-cluster echo "Hello from broker"
```

## Check status

```bash
broker status
```

```
NAME        STATUS  CLOUD  REGION     RESOURCES  NODES  LAUNCHED
my-cluster  INIT    aws    us-east-1  A100:1     1      2026-03-31T05:35:51Z
```

Or open the dashboard at `http://localhost:8080`.

## Run a job on an existing cluster

```bash
broker exec my-cluster python train.py
```

## SSH into a node

```bash
broker ssh my-cluster
```

## View logs

```bash
broker logs my-cluster
```

## Tear down

```bash
broker down my-cluster
```

## What's next

- [Architecture](../concepts/architecture) -- how the components work together
- [CLI reference](../reference/cli) -- all available commands
- [Task YAML reference](../reference/task-yaml) -- full spec for task files
- [Configuration](../reference/configuration) -- database backends and server settings
