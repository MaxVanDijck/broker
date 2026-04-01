---
title: broker
layout: hextra-home
---

<div style="max-width: 48rem; margin: 4rem auto; text-align: center; padding: 0 1.5rem;">

<h1 style="font-size: 3rem; font-weight: 800; letter-spacing: -0.03em; line-height: 1.1; margin-bottom: 1rem;">
Run AI workloads on<br/>any infrastructure
</h1>

<p style="font-size: 1.15rem; color: #999; margin-bottom: 2rem;">
One binary. Any cloud. Sub-second launch to SSH.
</p>

{{< hextra/hero-button text="Get Started" link="docs/getting-started" >}}

</div>

<div style="max-width: 36rem; margin: 2.5rem auto 3rem; padding: 0 1.5rem;">

```bash
$ broker launch -c train --gpus A100:8 task.yaml
Cluster train launched

$ broker ssh train
root@train-node-0:~#
```

</div>

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="Single binary, sub-50ms"
    subtitle="No Python, no pip, no dependencies. Server auto-starts on first command."
  >}}
  {{< hextra/feature-card
    title="Any cloud, any GPU"
    subtitle="AWS with GPU AMIs today. GCP, Azure, and Kubernetes coming soon."
  >}}
  {{< hextra/feature-card
    title="Zero-config SSH"
    subtitle="Tunneled through WebSocket. VS Code works via *.broker wildcard."
  >}}
  {{< hextra/feature-card
    title="Real-time dashboard"
    subtitle="SSE updates, CPU/memory/GPU charts, node details, one-click VS Code."
  >}}
{{< /hextra/feature-grid >}}
