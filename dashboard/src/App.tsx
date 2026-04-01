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
import { JobsPage } from "@/pages/jobs";
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
  path: "/clusters/$name",
  component: ClusterDetailPage,
});

const jobsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/jobs",
  component: JobsPage,
});

const routeTree = rootRoute.addChildren([indexRoute, clusterDetailRoute, jobsRoute]);
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
