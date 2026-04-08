import { useState, useEffect, useCallback } from "react";
import {
  createRouter,
  createRoute,
  createRootRoute,
  RouterProvider,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Layout } from "@/components/layout";
import { ClustersPage } from "@/pages/clusters";
import { ClusterDetailPage } from "@/pages/cluster-detail";
import { NodeDetailPage } from "@/pages/node-detail";
import { JobsPage } from "@/pages/jobs";
import { CostsPage } from "@/pages/costs";
import { ProvidersPage } from "@/pages/providers";
import { LoginPage } from "@/pages/login";
import { useServerEvents } from "@/lib/use-events";
import {
  AuthContext,
  getStoredToken,
  setStoredToken,
  clearStoredToken,
} from "@/lib/auth";

export const queryClient = new QueryClient();

const rootRoute = createRootRoute({ component: Layout });

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: ClustersPage,
});

const clusterDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/clusters/$id",
  component: ClusterDetailPage,
});

const nodeDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/clusters/$id/nodes/$nodeId",
  component: NodeDetailPage,
});

const jobsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/jobs",
  component: JobsPage,
});

const costsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/costs",
  component: CostsPage,
});

const providersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/providers",
  component: ProvidersPage,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  clusterDetailRoute,
  nodeDetailRoute,
  jobsRoute,
  costsRoute,
  providersRoute,
]);
const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

function EventSubscriber() {
  useServerEvents();
  return null;
}

type AuthStatus = "loading" | "authenticated" | "unauthenticated" | "noauth";

export default function App() {
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [token, setToken] = useState<string | null>(getStoredToken());

  const checkAuth = useCallback(async (t: string | null) => {
    try {
      const headers: Record<string, string> = {};
      if (t) {
        headers["Authorization"] = "Basic " + btoa("broker:" + t);
      }
      const res = await fetch("/api/v1/clusters", { headers });
      if (res.status === 401) {
        setStatus("unauthenticated");
      } else if (res.ok) {
        setStatus(t ? "authenticated" : "noauth");
      } else {
        setStatus("unauthenticated");
      }
    } catch {
      // Server unreachable -- show login so user can retry
      setStatus("unauthenticated");
    }
  }, []);

  useEffect(() => {
    checkAuth(token);
  }, [token, checkAuth]);

  const login = useCallback((newToken: string) => {
    setStoredToken(newToken);
    setToken(newToken);
    setStatus("authenticated");
  }, []);

  const logout = useCallback(() => {
    clearStoredToken();
    setToken(null);
    setStatus("unauthenticated");
    queryClient.clear();
  }, []);

  if (status === "loading") {
    return (
      <div className="flex h-screen items-center justify-center bg-neutral-950">
        <p className="text-sm text-neutral-500">Connecting...</p>
      </div>
    );
  }

  if (status === "unauthenticated") {
    return (
      <AuthContext.Provider value={{ token, login, logout }}>
        <LoginPage />
      </AuthContext.Provider>
    );
  }

  return (
    <AuthContext.Provider value={{ token, login, logout }}>
      <QueryClientProvider client={queryClient}>
        <EventSubscriber />
        <RouterProvider router={router} />
      </QueryClientProvider>
    </AuthContext.Provider>
  );
}
