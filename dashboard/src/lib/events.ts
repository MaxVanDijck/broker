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
  const source = new EventSource("/api/v1/events");

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
