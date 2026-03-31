---
title: REST API
weight: 5
---

The broker server exposes a REST API via the [Connect protocol](https://connectrpc.com/docs/protocol/). Every RPC is a POST request with a JSON body.

## Base URL

```
http://localhost:8080
```

## Endpoints

All endpoints follow the pattern: `POST /broker.v1.BrokerService/<Method>`

### Launch

Create a cluster and optionally submit a task.

```bash
curl -X POST http://localhost:8080/broker.v1.BrokerService/Launch \
  -H 'Content-Type: application/json' \
  -d '{
    "clusterName": "my-cluster",
    "task": {
      "name": "train",
      "run": "python train.py",
      "resources": {
        "accelerators": "A100:4"
      }
    }
  }'
```

Response:

```json
{
  "clusterName": "my-cluster",
  "headIp": "10.0.0.5"
}
```

### Status

List clusters.

```bash
curl -X POST http://localhost:8080/broker.v1.BrokerService/Status \
  -H 'Content-Type: application/json' \
  -d '{}'
```

Response:

```json
{
  "clusters": [
    {
      "name": "my-cluster",
      "status": "UP",
      "cloud": "aws",
      "region": "us-east-1",
      "numNodes": 1,
      "launchedAt": "2026-03-31T05:35:51Z"
    }
  ]
}
```

### Stop

```bash
curl -X POST http://localhost:8080/broker.v1.BrokerService/Stop \
  -H 'Content-Type: application/json' \
  -d '{"clusterName": "my-cluster"}'
```

### Start

```bash
curl -X POST http://localhost:8080/broker.v1.BrokerService/Start \
  -H 'Content-Type: application/json' \
  -d '{"clusterName": "my-cluster"}'
```

### Down

```bash
curl -X POST http://localhost:8080/broker.v1.BrokerService/Down \
  -H 'Content-Type: application/json' \
  -d '{"clusterName": "my-cluster"}'
```

### Exec

Submit a job to an existing cluster.

```bash
curl -X POST http://localhost:8080/broker.v1.BrokerService/Exec \
  -H 'Content-Type: application/json' \
  -d '{
    "clusterName": "my-cluster",
    "task": {
      "run": "python eval.py"
    }
  }'
```

### CancelJob

```bash
curl -X POST http://localhost:8080/broker.v1.BrokerService/CancelJob \
  -H 'Content-Type: application/json' \
  -d '{
    "clusterName": "my-cluster",
    "jobIds": ["abc123"],
    "all": false
  }'
```

## Health check

```bash
curl http://localhost:8080/healthz
# ok
```

## Protocol compatibility

The same endpoints also accept:

- **gRPC** (HTTP/2 with binary protobuf) -- used by the CLI
- **gRPC-web** (HTTP/1.1 compatible) -- used by browser dashboards
- **Connect** (HTTP/1.1 JSON POST) -- the examples above

All three are served by the same handler on the same port.
