---
title: broker
layout: hextra-home
---

{{< hextra/hero-badge link="https://github.com/broker-dev/broker" >}}
  {{< icon name="github" >}}
  Open Source on GitHub
{{< /hextra/hero-badge >}}

<div class="hx-mt-2 hx-mb-2">
{{< hextra/hero-headline >}}
  Run AI workloads&nbsp;<br class="sm:hx-block hx-hidden" />on any infrastructure
{{< /hextra/hero-headline >}}
</div>

<div class="hx-mb-4">
{{< hextra/hero-subtitle >}}
  Fast, unified compute orchestration for AI.&nbsp;<br class="sm:hx-block hx-hidden" />Written entirely in Go. Single binary. Zero dependencies on nodes.
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx-mb-2">
{{< hextra/hero-button text="Get Started" link="docs/getting-started" >}}
</div>

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="Sub-50ms CLI"
    subtitle="Single static Go binary. No Python, no pip, no virtual environments. Instant startup."
  >}}
  {{< hextra/feature-card
    title="Zero-dependency agent"
    subtitle="One binary on every node. Built-in SSH server, log streaming, Docker management. No Ray."
  >}}
  {{< hextra/feature-card
    title="Any infrastructure"
    subtitle="AWS, GCP, Azure, Kubernetes, on-prem SSH. One YAML spec runs anywhere."
  >}}
  {{< hextra/feature-card
    title="Agent connects outbound"
    subtitle="WebSocket tunnel from agent to server. No public IPs or inbound firewall rules needed."
  >}}
  {{< hextra/feature-card
    title="ConnectRPC API"
    subtitle="One port serves gRPC, gRPC-web, and plain HTTP. CLI, dashboard, and curl all work."
  >}}
  {{< hextra/feature-card
    title="Built-in SSH"
    subtitle="broker ssh my-cluster just works. VS Code Remote SSH support via ProxyCommand."
  >}}
{{< /hextra/feature-grid >}}
