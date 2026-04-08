import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { StatusBadge } from "@/components/status-badge";
import { authFetch } from "@/lib/auth";
import { DollarSign, RefreshCw, AlertTriangle } from "lucide-react";

interface CostCluster {
  cluster_name: string;
  cluster_id: string;
  hourly_rate: number;
  total_cost: number;
  is_spot: boolean;
  instance_type: string;
  status: string;
}

interface CostSummary {
  clusters: CostCluster[];
  total: number;
  disclaimer: string;
}

function formatUSD(value: number): string {
  return `$${value.toFixed(2)}`;
}

function formatRate(value: number): string {
  return `$${value.toFixed(2)}/hr`;
}

export function CostsPage() {
  const { data, isLoading, refetch, isFetching } = useQuery<CostSummary>({
    queryKey: ["costs"],
    queryFn: () =>
      authFetch("/api/v1/costs").then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
        return r.json();
      }),
    refetchInterval: 30000,
  });

  const navigate = useNavigate();
  const clusters = data?.clusters ?? [];
  const total = data?.total ?? 0;

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-xl font-semibold">Costs</h1>
        <button
          onClick={() => refetch()}
          disabled={isFetching}
          className="flex items-center gap-2 rounded-md bg-neutral-800 px-3 py-1.5 text-sm text-neutral-300 transition-colors hover:bg-neutral-700 disabled:opacity-50"
        >
          <RefreshCw className={`h-4 w-4 ${isFetching ? "animate-spin" : ""}`} />
          Refresh
        </button>
      </div>

      <div className="mb-6 flex items-start gap-3 rounded-lg border border-yellow-900/50 bg-yellow-950/20 p-4 text-sm text-yellow-400/80">
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
        <span>Cost estimates based on on-demand pricing. Actual AWS billing may differ.</span>
      </div>

      {isLoading ? (
        <div className="text-neutral-500">Loading...</div>
      ) : clusters.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed border-neutral-800 py-16 text-neutral-500">
          <DollarSign className="mb-4 h-10 w-10" />
          <p className="mb-1 text-sm">No cost data</p>
          <p className="text-xs text-neutral-600">
            Costs are tracked for clusters with known instance types.
          </p>
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-neutral-800">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-neutral-800 bg-neutral-900/50">
              <tr>
                <th className="px-4 py-3 font-medium text-neutral-400">ID</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Cluster</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Instance Type</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Hourly Rate</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Accumulated Cost</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {clusters.map((c) => (
                <tr
                  key={c.cluster_id}
                  onClick={() =>
                    navigate({ to: "/clusters/$id", params: { id: c.cluster_id } })
                  }
                  className="cursor-pointer transition-colors hover:bg-neutral-900/50"
                >
                  <td className="px-4 py-3">
                    <code className="text-xs font-medium text-white" title={c.cluster_id}>{c.cluster_id.slice(0, 8)}</code>
                  </td>
                  <td className="px-4 py-3 font-medium text-white">{c.cluster_name}</td>
                  <td className="px-4 py-3 font-mono text-xs text-neutral-400">
                    {c.instance_type}
                    {c.is_spot && (
                      <span className="ml-2 rounded bg-blue-900/40 px-1.5 py-0.5 text-[10px] text-blue-400">
                        spot
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-3 font-mono text-neutral-300">{formatRate(c.hourly_rate)}</td>
                  <td className="px-4 py-3 font-mono text-neutral-300">{formatUSD(c.total_cost)}</td>
                  <td className="px-4 py-3">
                    <StatusBadge status={c.status} />
                  </td>
                </tr>
              ))}
            </tbody>
            <tfoot className="border-t border-neutral-700 bg-neutral-900/30">
              <tr>
                <td className="px-4 py-3 font-medium text-neutral-300" colSpan={4}>
                  Total
                </td>
                <td className="px-4 py-3 font-mono font-medium text-white">
                  {formatUSD(total)}
                </td>
                <td />
              </tr>
            </tfoot>
          </table>
        </div>
      )}
    </div>
  );
}
