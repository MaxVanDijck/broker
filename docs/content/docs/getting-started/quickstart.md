---
title: Quickstart
weight: 2
---

This guide walks through launching a cluster, running a job, and SSHing into a node.

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

The server auto-starts in the background on first use -- no separate process to manage. SSH config is also auto-installed so `*.broker` hostnames work immediately.

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

Cluster status lifecycle: `INIT` -> `UP` -> `TERMINATING` -> `TERMINATED`

Clusters can also be stopped/started: `UP` -> `STOPPED` -> `UP`.

Or open the dashboard at `http://localhost:8080` for real-time updates with SSE, metrics charts (CPU, memory, GPU), and node detail pages.

## Sync a working directory

Upload your local project to the cluster:

```bash
broker launch -c my-cluster -w ~/my-project task.yaml
```

The `--workdir` / `-w` flag archives and uploads your directory to the node before running the task.

## SSH into a node

```bash
broker ssh my-cluster
```

SSH works through a WebSocket tunnel -- no public IP or firewall rules needed on the node. You can also use the auto-installed SSH config:

```bash
ssh my-cluster.broker
```

## Run a job on an existing cluster

```bash
broker exec my-cluster python train.py
```

## View logs

```bash
broker logs my-cluster
```

## Cancel a job

```bash
broker cancel my-cluster
broker cancel my-cluster --all
```

## Autostop

By default, clusters auto-terminate after 30 minutes of idle time. Override with `--autostop`:

```bash
broker launch -c my-cluster --autostop 1h task.yaml
broker launch -c my-cluster --autostop 0 task.yaml  # disable
```

## Tear down

```bash
broker down my-cluster
```

## What's next

- [Architecture](../concepts/architecture) -- how the components work together
- [CLI reference](../reference/cli) -- all available commands
- [Task YAML reference](../reference/task-yaml) -- full spec for task files
- [Configuration](../reference/configuration) -- server settings and storage backends
