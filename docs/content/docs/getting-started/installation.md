---
title: Installation
weight: 1
next: /docs/getting-started/quickstart
---

## From source

```bash
git clone https://github.com/broker-dev/broker.git
cd broker
make build
```

This produces three binaries in `bin/`:

```
bin/broker         # CLI
bin/broker-server  # Server (includes embedded dashboard)
bin/broker-agent   # Agent
```

## Go install

```bash
go install github.com/broker-dev/broker/cmd/broker@latest
go install github.com/broker-dev/broker/cmd/broker-server@latest
go install github.com/broker-dev/broker/cmd/broker-agent@latest
```

## Cross-compilation

The agent is typically deployed to Linux AMD64 nodes:

```bash
GOOS=linux GOARCH=amd64 go build -o bin/broker-agent-linux-amd64 ./cmd/broker-agent
```

## Verify installation

```bash
broker version
# broker v0.1.0
```
