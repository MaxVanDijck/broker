import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { broker } from "@/lib/api";
import { StatusBadge } from "@/components/status-badge";
import { Server, RefreshCw } from "lucide-react";

export function ClustersPage() {
  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ["clusters"],
    queryFn: () => broker.status({}),
    refetchInterval: 5000,
  });

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
                <th className="px-4 py-3 font-medium text-neutral-400">Name</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Status</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Cloud</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Region</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Resources</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Nodes</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Launched</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {clusters.map((c) => (
                <tr
                  key={c.name}
                  onClick={() => navigate({ to: "/clusters/$name", params: { name: c.name } })}
                  className="cursor-pointer transition-colors hover:bg-neutral-900/50"
                >
                  <td className="px-4 py-3 font-medium text-white">{c.name}</td>
                  <td className="px-4 py-3">
                    <StatusBadge status={c.status} />
                  </td>
                  <td className="px-4 py-3 text-neutral-400">{c.cloud || "-"}</td>
                  <td className="px-4 py-3 text-neutral-400">{c.region || "-"}</td>
                  <td className="px-4 py-3 text-neutral-400">
                    <code className="text-xs">{c.resources || "-"}</code>
                  </td>
                  <td className="px-4 py-3 text-neutral-400">{c.numNodes}</td>
                  <td className="px-4 py-3 text-neutral-400 text-xs">
                    {c.launchedAt ? new Date(c.launchedAt).toLocaleString() : "-"}
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
