---
title: Task YAML
weight: 2
---

Tasks are defined in YAML files. All fields are optional.

## Full spec

```yaml
# Display name for the task
name: my-training-job

# Working directory to sync to the node
workdir: ~/my-project

# Number of nodes (default: 1)
num_nodes: 1

# Resource requirements
resources:
  cloud: aws                # aws (gcp, azure, kubernetes coming soon)
  region: us-east-1
  zone: us-east-1a
  accelerators: A100:4      # GPU type and count
  cpus: "16+"               # Minimum CPU cores
  memory: "64+"             # Minimum memory in GB
  instance_type: p4d.24xlarge
  use_spot: false
  disk_size: 256            # Disk size in GB
  ports:
    - "8080"
  image_id: ami-xxx         # Cloud image or docker:<image>

# Environment variables
envs:
  WANDB_PROJECT: my-project
  HF_TOKEN: hf_xxx

# Setup script (runs once on first launch)
setup: |
  pip install -r requirements.txt

# Run script (the actual workload)
run: |
  torchrun --nproc_per_node=4 train.py
```

## Field reference

### Top-level

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | | Display name for the task |
| `workdir` | string | | Working directory to sync to the node. Can also be set via `--workdir` / `-w` flag. |
| `num_nodes` | int | `1` | Number of nodes to provision |
| `resources` | object | | Resource requirements (see below) |
| `envs` | map | | Environment variables |
| `setup` | string | | Setup script, runs once |
| `run` | string | | Run script, the main workload |

### Resources

| Field | Type | Default | Description |
|---|---|---|---|
| `cloud` | string | any | Cloud provider: `aws` (gcp, azure, kubernetes coming soon) |
| `region` | string | auto | Cloud region |
| `zone` | string | auto | Cloud zone |
| `accelerators` | string | | GPU spec, e.g. `A100:4`, `H100:8` |
| `cpus` | string | | CPU requirement, e.g. `4`, `16+` |
| `memory` | string | | Memory requirement, e.g. `32`, `64+` |
| `instance_type` | string | auto | Specific instance type |
| `use_spot` | bool | `false` | Use spot/preemptible instances |
| `disk_size` | int | | Disk size in GB |
| `ports` | list | | Ports to expose |
| `image_id` | string | | VM image ID or `docker:<image>`. For AWS GPU instances, defaults to the Deep Learning AMI. |

## Minimal examples

### Just a command

```yaml
run: echo "Hello from broker"
```

### GPU training

```yaml
name: train-llama
resources:
  accelerators: A100:8
setup: pip install -r requirements.txt
run: torchrun --nproc_per_node=8 train.py
```

### With workdir sync

```yaml
name: train-with-code
workdir: ~/my-project
resources:
  accelerators: A100:4
run: python train.py
```

### Specific cloud and region

```yaml
resources:
  cloud: aws
  region: us-east-1
  accelerators: H100:4
run: python inference.py
```
