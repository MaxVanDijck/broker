import { useParams, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { MetricsChart } from "@/components/metrics-chart";
import { ArrowLeft, Cpu, HardDrive } from "lucide-react";

interface MetricPoint {
  timestamp: string;
  node_id: string;
  cpu_percent: number;
  memory_percent: number;
  gpu_utilization: number;
  gpu_memory_used: number;
}

interface NodeInfo {
  node_id: string;
  hostname: string;
  status: string;
  cpus: number;
  memory_bytes: number;
  gpus: { model: string; memory_bytes: number }[];
  ssh_port: number;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
}

export function NodeDetailPage() {
  const { name, nodeId } = useParams({ from: "/clusters/$name/nodes/$nodeId" });

  const { data: nodesData } = useQuery<{ nodes: NodeInfo[] }>({
    queryKey: ["cluster-nodes", name],
    queryFn: () =>
      fetch(`/api/v1/clusters/${name}/nodes`).then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
        return r.json();
      }),
    refetchInterval: 30000,
  });

  const node = nodesData?.nodes?.find((n) => n.node_id === nodeId);

  const { data: metricsData } = useQuery<{ points: MetricPoint[] }>({
    queryKey: ["node-metrics", name, nodeId],
    queryFn: () => {
      const now = new Date();
      const from = new Date(now.getTime() - 30 * 60 * 1000);
      return fetch(
        `/api/v1/clusters/${name}/metrics?from=${from.toISOString()}&to=${now.toISOString()}&node_id=${nodeId}`,
      ).then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
        return r.json();
      });
    },
    refetchInterval: 15000,
  });

  const points = metricsData?.points ?? [];
  const nodeIds = useMemo(() => [nodeId], [nodeId]);

  return (
    <div>
      <div className="mb-6">
        <Link
          to="/clusters/$name"
          params={{ name }}
          className="mb-4 inline-flex items-center gap-1.5 text-sm text-neutral-500 transition-colors hover:text-neutral-300"
        >
          <ArrowLeft className="h-4 w-4" />
          {name}
        </Link>
        <h1 className="text-xl font-semibold">Node {nodeId}</h1>
        {node && (
          <p className="mt-1 text-sm text-neutral-500">
            {node.hostname || "unknown host"}
          </p>
        )}
      </div>

      {node && (
        <div className="mb-6 grid grid-cols-2 gap-4 sm:grid-cols-4">
          <InfoCard icon={Cpu} label="CPUs" value={String(node.cpus)} />
          <InfoCard icon={HardDrive} label="Memory" value={formatBytes(node.memory_bytes)} />
          <InfoCard
            icon={Cpu}
            label="GPUs"
            value={
              node.gpus.length === 0
                ? "None"
                : node.gpus.map((g) => g.model).join(", ")
            }
          />
          <InfoCard icon={HardDrive} label="Status" value={node.status} />
        </div>
      )}

      {points.length > 0 ? (
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
      ) : (
        <div className="rounded-lg border border-dashed border-neutral-800 py-12 text-center text-sm text-neutral-600">
          No metrics data yet
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
