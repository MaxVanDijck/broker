import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { StatusBadge } from "@/components/status-badge";
import { Activity, RefreshCw } from "lucide-react";

interface Job {
  id: string;
  cluster_id: string;
  cluster_name: string;
  name: string;
  status: string;
  user_id: string;
  submitted_at: string;
  started_at: string | null;
  ended_at: string | null;
}

function fetchJobs(cluster?: string): Promise<{ jobs: Job[] }> {
  const params = cluster ? `?cluster=${cluster}` : "";
  return fetch(`/api/v1/jobs${params}`).then((r) => {
    if (!r.ok) throw new Error(`${r.status}`);
    return r.json();
  });
}

function duration(job: Job): string {
  const start = job.started_at ? new Date(job.started_at) : new Date(job.submitted_at);
  const end = job.ended_at ? new Date(job.ended_at) : new Date();
  const ms = end.getTime() - start.getTime();

  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const mins = Math.floor(ms / 60_000);
  const secs = Math.floor((ms % 60_000) / 1000);
  return `${mins}m ${secs}s`;
}

function timeAgo(dateStr: string): string {
  const ms = Date.now() - new Date(dateStr).getTime();
  if (ms < 60_000) return "just now";
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m ago`;
  if (ms < 86_400_000) return `${Math.floor(ms / 3_600_000)}h ago`;
  return `${Math.floor(ms / 86_400_000)}d ago`;
}

export function JobsPage() {
  const navigate = useNavigate();
  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ["jobs"],
    queryFn: () => fetchJobs(),
    refetchInterval: 30000,
  });

  const jobs = data?.jobs ?? [];

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-xl font-semibold">Jobs</h1>
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
      ) : jobs.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed border-neutral-800 py-16 text-neutral-500">
          <Activity className="mb-4 h-10 w-10" />
          <p className="mb-1 text-sm">No jobs yet</p>
          <p className="text-xs text-neutral-600">
            Submit one with{" "}
            <code className="rounded bg-neutral-800 px-1.5 py-0.5 text-neutral-300">
              broker exec my-cluster echo hello
            </code>
          </p>
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-neutral-800">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-neutral-800 bg-neutral-900/50">
              <tr>
                <th className="px-4 py-3 font-medium text-neutral-400">ID</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Status</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Cluster</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Name</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Duration</th>
                <th className="px-4 py-3 font-medium text-neutral-400">Submitted</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {jobs.map((j) => (
                <tr
                  key={j.id}
                  onClick={() =>
                    navigate({
                      to: "/clusters/$id",
                      params: { id: j.cluster_id },
                    })
                  }
                  className="cursor-pointer transition-colors hover:bg-neutral-900/50"
                >
                  <td className="px-4 py-3">
                    <code className="text-xs font-medium text-white">{j.id}</code>
                  </td>
                  <td className="px-4 py-3">
                    <StatusBadge status={j.status} />
                  </td>
                  <td className="px-4 py-3 text-neutral-400">{j.cluster_name}</td>
                  <td className="px-4 py-3 text-neutral-400">{j.name || "-"}</td>
                  <td className="px-4 py-3 font-mono text-xs text-neutral-400">
                    {duration(j)}
                  </td>
                  <td className="px-4 py-3 text-xs text-neutral-500">
                    {timeAgo(j.submitted_at)}
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
