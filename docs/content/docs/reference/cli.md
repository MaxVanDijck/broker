---
title: CLI Reference
weight: 1
---

## broker launch

Launch a cluster or submit a task. The server auto-starts in the background if not already running.

```bash
broker launch [flags] [yaml-or-command...]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--cluster` | `-c` | auto-generated | Cluster name |
| `--gpus` | | | GPU type and count (e.g. `A100:4`) |
| `--cloud` | | | Cloud provider |
| `--workdir` | `-w` | | Working directory to upload to the node |
| `--detach-run` | `-d` | `false` | Detach after job submission |
| `--region` | | | AWS region |
| `--instance-type` | | | EC2 instance type |
| `--spot` | | `false` | Use spot instances |
| `--disk-size` | | | Disk size in GB |
| `--autostop` | | `30m` | Idle duration before auto-teardown (0 to disable) |

**Examples:**

```bash
# Launch from a YAML task file
broker launch -c train task.yaml

# Launch with an inline command
broker launch -c dev echo "hello world"

# Launch with GPU override
broker launch -c train --gpus H100:8 task.yaml

# Launch with workdir sync
broker launch -c train -w ~/my-project task.yaml

# Auto-generated cluster name
broker launch task.yaml

# Disable autostop
broker launch -c train --autostop 0 task.yaml
```

---

## broker status

Show cluster status.

```bash
broker status [flags] [clusters...]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--refresh` | `-r` | `false` | Refresh status from cloud provider |

Cluster status lifecycle: `INIT` -> `UP` -> `TERMINATING` -> `TERMINATED`. Stopped clusters: `UP` -> `STOPPED` -> `UP`.

---

## broker stop

Stop a cluster. Nodes are preserved but stopped.

```bash
broker stop CLUSTER
```

---

## broker start

Start a stopped cluster.

```bash
broker start CLUSTER
```

---

## broker down

Tear down a cluster. Releases all resources.

```bash
broker down [flags] CLUSTER
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--yes` | `-y` | `false` | Skip confirmation prompt |

---

## broker exec

Execute a task on an existing cluster.

```bash
broker exec CLUSTER [yaml-or-command...]
```

**Examples:**

```bash
broker exec my-cluster python train.py
broker exec my-cluster task.yaml
```

---

## broker logs

Stream logs from a job.

```bash
broker logs [flags] CLUSTER [JOB_ID]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--follow` | `-f` | `false` | Follow log output |

---

## broker cancel

Cancel jobs on a cluster.

```bash
broker cancel [flags] CLUSTER [JOB_IDS...]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--all` | `-a` | `false` | Cancel all jobs |

---

## broker jobs

List jobs.

```bash
broker jobs [flags]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--cluster` | `-c` | | Filter by cluster name |

---

## broker login

Authenticate with an OIDC provider (Okta, Google, Auth0, Azure AD).

```bash
broker login [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--issuer` | `$BROKER_OIDC_ISSUER` | OIDC issuer URL (e.g. `https://dev-123456.okta.com`) |
| `--client-id` | `$BROKER_OIDC_CLIENT_ID` | OAuth2 client ID |

Opens a browser for authentication. Tokens are stored in `~/.broker/credentials.json` and automatically refreshed.

---

## broker logout

Clear stored authentication credentials.

```bash
broker logout
```

---

## broker ssh

SSH into a cluster node. Tunnels through the server via WebSocket -- nodes do not need public IPs.

```bash
broker ssh [flags] CLUSTER
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--user` | `-l` | `root` | SSH user |
| `--port` | `-p` | `2222` | SSH port |
| `--ssh-flag` | `-o` | | Extra SSH flags |
| `--stdio` | | `false` | Proxy SSH over stdin/stdout (for ProxyCommand) |

SSH config is auto-installed on first CLI use. After that, you can use:

```bash
ssh my-cluster.broker
```

The `*.broker` wildcard in `~/.ssh/config` routes through the broker CLI's ProxyCommand automatically. VS Code Remote SSH also works -- connect to `<cluster>.broker`.

---

## broker ssh-config

Manually install/reinstall the SSH config. This is normally auto-installed on first CLI use.

```bash
broker ssh-config
```

Writes a `~/.broker/ssh_config` file and adds an `Include` directive to `~/.ssh/config`.

---

## broker version

Print the broker version.

```bash
broker version
```

---

## Environment variables

| Variable | Description |
|---|---|
| `BROKER_API_ADDR` | Server address (default: `http://localhost:8080`) |
| `BROKER_TOKEN` | Shared secret for Basic auth (service accounts, CI) |
| `BROKER_OIDC_ISSUER` | Default OIDC issuer URL for `broker login` |
| `BROKER_OIDC_CLIENT_ID` | Default OIDC client ID for `broker login` |
