import { useParams, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState, useCallback } from "react";
import { broker } from "@/lib/api";
import { StatusBadge } from "@/components/status-badge";
import { LogViewer } from "@/components/log-viewer";
import { MetricsChart } from "@/components/metrics-chart";
import {
  ArrowLeft,
  Server,
  Cpu,
  HardDrive,
  Globe,
  Terminal,
  Monitor,
  ExternalLink,
} from "lucide-react";

interface NodeInfo {
  node_id: string;
  hostname: string;
  status: string;
  cpus: number;
  memory_bytes: number;
  gpus: { model: string; memory_bytes: number }[];
  ssh_port: number;
}

interface MetricPoint {
  timestamp: string;
  node_id: string;
  cpu_percent: number;
  memory_percent: number;
  gpu_utilization: number;
  gpu_memory_used: number;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
}

export function ClusterDetailPage() {
  const { name } = useParams({ from: "/clusters/$name" });

  const { data: statusData } = useQuery({
    queryKey: ["cluster", name],
    queryFn: () => broker.status({ clusterNames: [name] }),
    refetchInterval: 5000,
  });

  const cluster = statusData?.clusters?.[0];

  const vscodeURL = `vscode://vscode-remote/ssh-remote+${name}.broker`;

  return (
    <div>
      <div className="mb-6">
        <Link
          to="/"
          className="mb-4 inline-flex items-center gap-1.5 text-sm text-neutral-500 transition-colors hover:text-neutral-300"
        >
          <ArrowLeft className="h-4 w-4" />
          Clusters
        </Link>
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-semibold">{name}</h1>
          {cluster && <StatusBadge status={cluster.status} />}
        </div>
      </div>

      {cluster ? (
        <div className="space-y-6">
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            <InfoCard icon={Globe} label="Cloud" value={cluster.cloud || "any"} />
            <InfoCard icon={Globe} label="Region" value={cluster.region || "-"} />
            <InfoCard icon={Cpu} label="Resources" value={cluster.resources || "-"} />
            <InfoCard icon={HardDrive} label="Nodes" value={String(cluster.numNodes)} />
          </div>

          <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
            <div>
              <h2 className="mb-3 text-sm font-medium text-neutral-400">Actions</h2>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <CopyButton
                    label="SSH"
                    icon={Terminal}
                    copyText={`broker ssh ${name}`}
                    notification="Copied to clipboard"
                  />
                  <a
                    href={vscodeURL}
                    className="flex items-center gap-2 rounded-md bg-neutral-800 px-3 py-1.5 text-sm text-neutral-300 transition-colors hover:bg-neutral-700"
                  >
                    <Monitor className="h-4 w-4" />
                    Open in VS Code
                    <ExternalLink className="h-3 w-3 text-neutral-500" />
                  </a>
                </div>
                <ActionButton
                  label="Tear Down"
                  icon={Server}
                  onClick={async () => {
                    if (confirm(`Tear down cluster ${name}?`)) {
                      await broker.down({ clusterName: name });
                    }
                  }}
                  variant="danger"
                />
              </div>
            </div>

            <div>
              <h2 className="mb-3 text-sm font-medium text-neutral-400">Info</h2>
              <div className="rounded-lg border border-neutral-800 bg-neutral-900/30 p-4 text-sm">
                <div className="grid grid-cols-2 gap-y-2">
                  <span className="text-neutral-500">Launched</span>
                  <span className="text-neutral-300">
                    {cluster.launchedAt ? new Date(cluster.launchedAt).toLocaleString() : "-"}
                  </span>
                  <span className="text-neutral-500">Head IP</span>
                  <span className="font-mono text-neutral-300">{cluster.headIp || "-"}</span>
                  <span className="text-neutral-500">SSH</span>
                  <span className="font-mono text-neutral-300">{name}.broker</span>
                </div>
              </div>
            </div>
          </div>

          <NodesSection clusterName={name} />

          <div>
            <h2 className="mb-3 text-sm font-medium text-neutral-400">Logs</h2>
            <LogViewer clusterName={name} />
          </div>
        </div>
      ) : (
        <div className="text-neutral-500">Loading cluster...</div>
      )}
    </div>
  );
}

function NodesSection({ clusterName }: { clusterName: string }) {
  const { data: nodesData } = useQuery<{ nodes: NodeInfo[] }>({
    queryKey: ["cluster-nodes", clusterName],
    queryFn: () =>
      fetch(`/api/v1/clusters/${clusterName}/nodes`).then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
        return r.json();
      }),
    refetchInterval: 15000,
  });

  const { data: metricsData } = useQuery<{ points: MetricPoint[] }>({
    queryKey: ["cluster-metrics", clusterName],
    queryFn: () => {
      const now = new Date();
      const from = new Date(now.getTime() - 30 * 60 * 1000);
      return fetch(
        `/api/v1/clusters/${clusterName}/metrics?from=${from.toISOString()}&to=${now.toISOString()}`,
      ).then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
        return r.json();
      });
    },
    refetchInterval: 15000,
  });

  const nodes = nodesData?.nodes ?? [];
  const points = metricsData?.points ?? [];

  const nodeIds = useMemo(() => {
    const ids = new Set<string>();
    for (const n of nodes) ids.add(n.node_id);
    for (const p of points) ids.add(p.node_id);
    return Array.from(ids).sort();
  }, [nodes, points]);

  return (
    <div className="space-y-4">
      <h2 className="text-sm font-medium text-neutral-400">Nodes</h2>

      {nodes.length > 0 && (
        <div className="overflow-hidden rounded-lg border border-neutral-800">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-neutral-800 bg-neutral-900/50">
              <tr>
                <th className="px-4 py-3 font-medium text-neutral-400">Node ID</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Hostname</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Status</th>
                <th className="px-4 py-3 font-medium text-neutral-400">CPUs</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Memory</th>
                <th className="px-4 py-3 font-medium text-neutral-400">GPUs</th>
                <th className="px-4 py-3 font-medium text-neutral-400">SSH Port</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {nodes.map((node) => (
                <tr key={node.node_id} className="transition-colors hover:bg-neutral-900/50">
                  <td className="px-4 py-3 font-mono text-sm text-white">{node.node_id}</td>
                  <td className="px-4 py-3 text-neutral-400">{node.hostname || "-"}</td>
                  <td className="px-4 py-3">
                    <StatusBadge status={node.status.toUpperCase()} />
                  </td>
                  <td className="px-4 py-3 text-neutral-400">{node.cpus}</td>
                  <td className="px-4 py-3 text-neutral-400">{formatBytes(node.memory_bytes)}</td>
                  <td className="px-4 py-3 text-neutral-400">
                    {node.gpus.length === 0
                      ? "-"
                      : node.gpus.map((g) => `${g.model} (${formatBytes(g.memory_bytes)})`).join(", ")}
                  </td>
                  <td className="px-4 py-3 font-mono text-neutral-400">{node.ssh_port || "-"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {nodes.length === 0 && (
        <div className="rounded-lg border border-dashed border-neutral-800 py-8 text-center text-sm text-neutral-600">
          No nodes connected
        </div>
      )}

      {points.length > 0 && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <MetricsChart
            title="CPU %"
            points={points}
            nodeIds={nodeIds}
            dataKey="cpu_percent"
            unit="%"
          />
          <MetricsChart
            title="Memory %"
            points={points}
            nodeIds={nodeIds}
            dataKey="memory_percent"
            unit="%"
          />
          <MetricsChart
            title="GPU Utilization %"
            points={points}
            nodeIds={nodeIds}
            dataKey="gpu_utilization"
            unit="%"
          />
          <MetricsChart
            title="GPU Memory Used"
            points={points}
            nodeIds={nodeIds}
            dataKey="gpu_memory_used"
            formatValue={(v: number) => formatBytes(v)}
          />
        </div>
      )}
    </div>
  );
}

function InfoCard({
  icon: Icon,
  label,
  value,
}: {
  icon: React.ElementType;
  label: string;
  value: string;
}) {
  return (
    <div className="rounded-lg border border-neutral-800 bg-neutral-900/30 p-4">
      <div className="mb-2 flex items-center gap-2 text-neutral-500">
        <Icon className="h-4 w-4" />
        <span className="text-xs">{label}</span>
      </div>
      <div className="truncate font-mono text-sm text-neutral-200">{value}</div>
    </div>
  );
}

function CopyButton({
  label,
  icon: Icon,
  copyText,
  notification,
}: {
  label: string;
  icon: React.ElementType;
  copyText: string;
  notification: string;
}) {
  const [showNotification, setShowNotification] = useState(false);

  const handleClick = useCallback(() => {
    navigator.clipboard.writeText(copyText);
    setShowNotification(true);
    setTimeout(() => setShowNotification(false), 2000);
  }, [copyText]);

  return (
    <span className="relative inline-flex items-center gap-2">
      <button
        onClick={handleClick}
        className="flex items-center gap-2 rounded-md bg-neutral-800 px-3 py-1.5 text-sm text-neutral-300 transition-colors hover:bg-neutral-700"
      >
        <Icon className="h-4 w-4" />
        {label}
      </button>
      {showNotification && (
        <span className="animate-fade-in whitespace-nowrap text-xs text-green-400">
          {notification}
        </span>
      )}
    </span>
  );
}

function ActionButton({
  label,
  icon: Icon,
  onClick,
  variant,
}: {
  label: string;
  icon: React.ElementType;
  onClick: () => void;
  variant: "default" | "danger";
}) {
  const base = "flex items-center gap-2 rounded-md px-3 py-1.5 text-sm transition-colors";
  const styles =
    variant === "danger"
      ? `${base} bg-red-950/50 text-red-400 border border-red-900 hover:bg-red-950`
      : `${base} bg-neutral-800 text-neutral-300 hover:bg-neutral-700`;

  return (
    <button onClick={onClick} className={styles}>
      <Icon className="h-4 w-4" />
      {label}
    </button>
  );
}
