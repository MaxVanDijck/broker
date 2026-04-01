import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { connectEvents } from "./events";
import type { ServerEvent } from "./events";

export function useServerEvents() {
  const queryClient = useQueryClient();

  useEffect(() => {
    const disconnect = connectEvents((event: ServerEvent) => {
      switch (event.type) {
        case "cluster_update":
          queryClient.invalidateQueries({ queryKey: ["clusters"] });
          if (event.data.cluster_name) {
            queryClient.invalidateQueries({
              queryKey: ["cluster", event.data.cluster_name],
            });
          }
          break;
        case "node_online":
        case "node_offline":
          queryClient.invalidateQueries({ queryKey: ["clusters"] });
          if (event.data.cluster_name) {
            queryClient.invalidateQueries({
              queryKey: ["cluster", event.data.cluster_name],
            });
            queryClient.invalidateQueries({
              queryKey: ["cluster-nodes", event.data.cluster_name],
            });
            queryClient.invalidateQueries({
              queryKey: ["cluster-nodes-header", event.data.cluster_name],
            });
          }
          break;
        case "job_update":
          queryClient.invalidateQueries({ queryKey: ["jobs"] });
          if (event.data.cluster_name) {
            queryClient.invalidateQueries({
              queryKey: ["cluster", event.data.cluster_name],
            });
          }
          break;
      }
    });

    return disconnect;
  }, [queryClient]);
}
