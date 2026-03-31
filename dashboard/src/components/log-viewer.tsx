import { useEffect, useRef, useState } from "react";
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { BrokerService } from "@/gen/broker_pb";

const transport = createConnectTransport({
  baseUrl: window.location.origin,
});

const client = createClient(BrokerService, transport);

interface LogLine {
  timestamp: string;
  text: string;
}

export function LogViewer({ clusterName, jobId }: { clusterName: string; jobId?: string }) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [connected, setConnected] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let cancelled = false;

    async function stream() {
      setConnected(true);
      try {
        const response = client.logs({
          clusterName,
          jobId: jobId ?? "",
          follow: true,
        });

        for await (const msg of response) {
          if (cancelled) break;
          setLines((prev) => [
            ...prev,
            {
              timestamp: new Date().toISOString().substring(11, 23),
              text: msg.line,
            },
          ]);
        }
      } catch {
        // stream ended
      } finally {
        if (!cancelled) setConnected(false);
      }
    }

    stream();
    return () => {
      cancelled = true;
    };
  }, [clusterName, jobId]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [lines]);

  return (
    <div
      ref={containerRef}
      className="relative overflow-hidden rounded-lg border border-neutral-800 bg-neutral-950"
    >
      <div className="flex items-center justify-between border-b border-neutral-800 px-4 py-2">
        <span className="text-xs text-neutral-500">
          {connected ? "streaming" : "disconnected"}
        </span>
        <div className="flex items-center gap-2">
          <div
            className={`h-2 w-2 rounded-full ${
              connected ? "bg-green-500" : "bg-neutral-600"
            }`}
          />
          <button
            onClick={() => setLines([])}
            className="text-xs text-neutral-500 hover:text-neutral-300"
          >
            Clear
          </button>
        </div>
      </div>
      <div className="h-80 overflow-y-auto p-4 font-mono text-xs leading-relaxed">
        {lines.length === 0 ? (
          <span className="text-neutral-600">Waiting for logs...</span>
        ) : (
          lines.map((line, i) => (
            <div key={i} className="flex gap-3">
              <span className="shrink-0 select-none text-neutral-600">
                {line.timestamp}
              </span>
              <span className="whitespace-pre-wrap text-neutral-300">{line.text}</span>
            </div>
          ))
        )}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}
