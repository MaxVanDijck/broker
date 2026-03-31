---
title: VS Code Remote SSH
weight: 1
---

broker's built-in SSH server integrates with VS Code Remote SSH for a seamless development experience on remote GPU nodes.

## Setup

Add the following to `~/.ssh/config`:

```
Host broker-*
  ProxyCommand broker ssh --stdio %h
  User root
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
```

## Usage

1. Launch a cluster:

```bash
broker launch -c dev-box --gpus A100:1
```

2. Open VS Code and use the Remote SSH extension to connect to `broker-dev-box`.

3. VS Code will connect through the broker CLI, which tunnels the SSH connection through the server to the agent's built-in SSH server.

## How it works

```
VS Code -> ssh broker-dev-box
           -> ProxyCommand: broker ssh --stdio broker-dev-box
              -> broker CLI connects to server
                 -> server routes to agent WebSocket tunnel
                    -> agent's built-in SSH server (port 2222)
```

The agent's SSH server supports:

- PTY sessions with window resizing
- Non-PTY exec (used by VS Code for probing and file operations)
- Local port forwarding (access services running on the node)
- Reverse port forwarding

## Port forwarding

VS Code will automatically forward ports. You can also manually forward:

```bash
ssh -L 8888:localhost:8888 broker-dev-box
```

This forwards port 8888 on your local machine to port 8888 on the remote node, useful for Jupyter notebooks or TensorBoard.
