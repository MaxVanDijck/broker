---
title: Jobs
weight: 3
---

A job is a unit of work submitted to a cluster. Jobs go through a defined lifecycle and their logs are streamed back to the server in real time.

## Lifecycle

```
PENDING -> SETUP -> RUNNING -> SUCCEEDED
                          |-> FAILED
                          |-> CANCELLED
```

| State | Description |
|---|---|
| `PENDING` | Job accepted, waiting for the agent to pick it up |
| `SETUP` | Running the `setup` script (e.g. `pip install`) |
| `RUNNING` | Running the `run` script |
| `SUCCEEDED` | Completed with exit code 0 |
| `FAILED` | Completed with non-zero exit code or error |
| `CANCELLED` | Cancelled by user via `broker cancel` |

## Submitting jobs

Jobs are submitted via `broker launch` (creates cluster + submits job) or `broker exec` (submits to existing cluster):

```bash
# Launch a new cluster and run a job
broker launch -c train task.yaml

# Submit a job to an existing cluster
broker exec train python train.py

# Submit from a YAML file
broker exec train task.yaml
```

## Viewing logs

```bash
# Follow logs of the latest job
broker logs my-cluster

# Follow logs of a specific job
broker logs my-cluster abc123
```

## Cancelling jobs

```bash
# Cancel a specific job
broker cancel my-cluster abc123

# Cancel all jobs on a cluster
broker cancel my-cluster --all
```
