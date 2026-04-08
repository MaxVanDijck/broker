import { getStoredToken } from "./auth";

export type ServerEventType =
  | "cluster_update"
  | "node_online"
  | "node_offline"
  | "job_update";

export interface ServerEvent {
  type: ServerEventType;
  data: Record<string, string>;
}

export type EventHandler = (event: ServerEvent) => void;

export function connectEvents(onEvent: EventHandler): () => void {
  const token = getStoredToken();
  let url = "/api/v1/events";
  if (token) {
    url += "?token=" + encodeURIComponent(token);
  }

  const source = new EventSource(url);

  const eventTypes: ServerEventType[] = [
    "cluster_update",
    "node_online",
    "node_offline",
    "job_update",
  ];

  for (const type of eventTypes) {
    source.addEventListener(type, (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data);
        onEvent({ type, data });
      } catch {
        // ignore malformed events
      }
    });
  }

  return () => {
    source.close();
  };
}
