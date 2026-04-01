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
import { useServerEvents } from "@/lib/use-events";

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

const routeTree = rootRoute.addChildren([indexRoute, clusterDetailRoute, nodeDetailRoute, jobsRoute, costsRoute]);
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

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <EventSubscriber />
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
