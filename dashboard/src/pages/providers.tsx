import { useQuery } from "@tanstack/react-query";
import { Cloud, RefreshCw } from "lucide-react";

interface PreflightCheck {
  name: string;
  status: "ok" | "error";
  message: string;
}

interface ProviderResult {
  cloud: string;
  status: "healthy" | "unhealthy";
  checks: PreflightCheck[];
}

interface ProvidersResponse {
  providers: ProviderResult[];
}

export function ProvidersPage() {
  const { data, isLoading, refetch, isFetching } = useQuery<ProvidersResponse>({
    queryKey: ["providers"],
    queryFn: () =>
      fetch("/api/v1/providers").then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
        return r.json();
      }),
    refetchInterval: 60000,
  });

  const providers = data?.providers ?? [];

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-xl font-semibold">Cloud Providers</h1>
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
      ) : providers.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed border-neutral-800 py-16 text-neutral-500">
          <Cloud className="mb-4 h-10 w-10" />
          <p className="mb-1 text-sm">No cloud providers configured</p>
          <p className="text-xs text-neutral-600">
            Add AWS credentials to get started.
          </p>
        </div>
      ) : (
        <div className="grid gap-4">
          {providers.map((provider) => (
            <div key={provider.cloud} className="rounded-lg border border-neutral-800 p-6">
              <div className="mb-4 flex items-center gap-3">
                <div className={`h-3 w-3 rounded-full ${provider.status === "healthy" ? "bg-emerald-500" : "bg-red-500"}`} />
                <h2 className="text-lg font-semibold">{provider.cloud.toUpperCase()}</h2>
                <span className={`text-sm ${provider.status === "healthy" ? "text-emerald-400" : "text-red-400"}`}>
                  {provider.status}
                </span>
              </div>
              <div className="space-y-2">
                {provider.checks.map((check) => (
                  <div key={check.name} className="flex items-start gap-2 text-sm">
                    <div className={`mt-1 h-2 w-2 shrink-0 rounded-full ${check.status === "ok" ? "bg-emerald-500" : "bg-red-500"}`} />
                    <div>
                      <span className="font-medium text-neutral-300">{check.name}</span>
                      <span className="ml-2 text-neutral-500">{check.message}</span>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
