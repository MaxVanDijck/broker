import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { StatusBadge } from "@/components/status-badge";
import { authFetch } from "@/lib/auth";
import { Server, RefreshCw } from "lucide-react";

interface Cluster {
  id: string;
  name: string;
  status: string;
  cloud: string;
  region: string;
  resources: string;
  head_ip: string;
  num_nodes: number;
  launched_at: string;
  instance_type: string;
  is_spot: boolean;
}

interface CostCluster {
  cluster_id: string;
  hourly_rate: number;
}

function fetchClusters(): Promise<{ clusters: Cluster[] }> {
  return authFetch("/api/v1/clusters").then((r) => {
    if (!r.ok) throw new Error(`${r.status}`);
    return r.json();
  });
}

export function ClustersPage() {
  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ["clusters"],
    queryFn: fetchClusters,
    refetchInterval: 30000,
  });

  const { data: costData } = useQuery<{ clusters: CostCluster[] }>({
    queryKey: ["costs"],
    queryFn: () =>
      authFetch("/api/v1/costs").then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
        return r.json();
      }),
    refetchInterval: 30000,
  });

  const costById = new Map(
    (costData?.clusters ?? []).map((c) => [c.cluster_id, c]),
  );

  const navigate = useNavigate();
  const clusters = data?.clusters ?? [];

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-xl font-semibold">Clusters</h1>
        <button
          onClick={() => refetch()}
          disabled={isFetching}
          className="flex items-center gap-2 rounded-md bg-neutral-800 px-3 py-1.5 text-sm text-neutral-300 transition-colors hover:bg-neutral-700 disabled:opacity-50"
        >
          <RefreshCw className={`h-4 w-4 ${isFetching ? "animate-spin" : ""}`} />
          Refresh
        </button>
      </div>

      {isLoading ? (
        <div className="text-neutral-500">Loading...</div>
      ) : clusters.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed border-neutral-800 py-16 text-neutral-500">
          <Server className="mb-4 h-10 w-10" />
          <p className="mb-1 text-sm">No clusters</p>
          <p className="text-xs text-neutral-600">
            Launch one with <code className="rounded bg-neutral-800 px-1.5 py-0.5 text-neutral-300">broker launch</code>
          </p>
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-neutral-800">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-neutral-800 bg-neutral-900/50">
              <tr>
                <th className="px-4 py-3 font-medium text-neutral-400">ID</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Name</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Status</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Cloud</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Region</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Resources</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Nodes</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Cost</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Launched</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {clusters.map((c) => (
                <tr
                  key={c.id}
                  onClick={() => navigate({ to: "/clusters/$id", params: { id: c.id } })}
                  className="cursor-pointer transition-colors hover:bg-neutral-900/50"
                >
                  <td className="px-4 py-3">
                    <code className="text-xs font-medium text-white" title={c.id}>
                      {c.id.slice(0, 8)}
                    </code>
                  </td>
                  <td className="px-4 py-3 font-medium text-white">{c.name}</td>
                  <td className="px-4 py-3">
                    <StatusBadge status={c.status} />
                  </td>
                  <td className="px-4 py-3 text-neutral-400">{c.cloud || "-"}</td>
                  <td className="px-4 py-3 text-neutral-400">{c.region || "-"}</td>
                  <td className="px-4 py-3 text-neutral-400">
                    <code className="text-xs">{c.resources || "-"}</code>
                  </td>
                  <td className="px-4 py-3 text-neutral-400">{c.num_nodes}</td>
                  <td className="px-4 py-3 font-mono text-neutral-400">
                    {costById.has(c.id) ? `$${costById.get(c.id)!.hourly_rate.toFixed(2)}/hr` : "-"}
                  </td>
                  <td className="px-4 py-3 text-neutral-400 text-xs">
                    {c.launched_at ? new Date(c.launched_at).toLocaleString() : "-"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
