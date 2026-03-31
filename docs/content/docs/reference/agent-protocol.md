---
title: Agent Protocol
weight: 4
---

The agent communicates with the server over a WebSocket connection using protobuf-encoded messages.

## Connection

The agent connects to `ws://<server>/agent/v1/connect` and sends binary WebSocket frames. Each frame contains a serialized `Envelope` protobuf message.

## Handshake

1. Agent sends `Register` with node ID, cluster name, auth token, and node info
2. Server responds with `RegisterAck` containing the heartbeat interval
3. If rejected, the agent backs off and retries

## Message types

### Agent -> Server

#### Register

Sent once on connection.

```protobuf
message Register {
  string node_id = 1;
  string cluster_name = 2;
  string token = 3;
  NodeInfo node_info = 4;
}
```

#### Heartbeat

Sent periodically (default: every 15 seconds).

```protobuf
message Heartbeat {
  string node_id = 1;
  int64 timestamp_unix = 2;
  double cpu_percent = 3;
  double memory_percent = 4;
  int64 disk_used_bytes = 5;
  repeated GPUMetrics gpu_metrics = 6;
  repeated string running_job_ids = 7;
}
```

#### LogBatch

Batched log entries from running jobs.

```protobuf
message LogBatch {
  string job_id = 1;
  repeated LogEntry entries = 2;
}

message LogEntry {
  int64 timestamp_unix_nano = 1;
  string stream = 2;       // "stdout" or "stderr"
  bytes data = 3;
}
```

#### JobUpdate

Job state changes.

```protobuf
message JobUpdate {
  string job_id = 1;
  JobState state = 2;
  int32 exit_code = 3;
  string error = 4;
}
```

### Server -> Agent

#### RegisterAck

```protobuf
message RegisterAck {
  bool accepted = 1;
  string error = 2;
  int32 heartbeat_interval_seconds = 3;
}
```

#### SubmitJob

```protobuf
message SubmitJob {
  string job_id = 1;
  string name = 2;
  string image = 3;               // Docker image (host mode)
  repeated string command = 4;
  map<string, string> env = 5;
  repeated string ports = 6;
  repeated string gpu_ids = 7;
  string setup_script = 8;        // Run mode
  string run_script = 9;          // Run mode
  string workdir = 10;
}
```

#### CancelJob

```protobuf
message CancelJob {
  string job_id = 1;
  bool force = 2;
}
```

#### TerminateNode

```protobuf
message TerminateNode {
  string reason = 1;
  int32 grace_period_seconds = 2;
}
```

## Reconnection

If the WebSocket connection drops, the agent reconnects with exponential backoff:

- Initial delay: 1 second
- Multiplier: 2x
- Maximum delay: 30 seconds
- No maximum retry count (retries indefinitely)

On reconnection, the agent re-sends `Register`. The server replaces the old connection state.

## Envelope

All messages are wrapped in an `Envelope` with a `oneof` payload:

```protobuf
message Envelope {
  oneof payload {
    Register register = 1;
    Heartbeat heartbeat = 2;
    LogBatch log_batch = 3;
    JobUpdate job_update = 4;
    RegisterAck register_ack = 20;
    SubmitJob submit_job = 21;
    CancelJob cancel_job = 22;
    TerminateNode terminate_node = 23;
  }
}
```
