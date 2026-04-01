---
title: Installation
weight: 1
next: /docs/getting-started/quickstart
---

## From source

```bash
git clone https://github.com/MaxVanDijck/broker.git
cd broker
make build
```

This produces a single binary:

```
bin/broker         # CLI + embedded server
```

The CLI auto-starts the server as a background process on first use. No separate `broker-server` process is needed for local development.

For production deployments, the server can also be built separately:

```bash
make build-server  # bin/broker-server
make build-agent   # bin/broker-agent
```

## Verify installation

```bash
broker version
# broker v0.1.0
```
