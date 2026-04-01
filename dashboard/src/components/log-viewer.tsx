import { useEffect, useRef, useState, useCallback } from "react";
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { BrokerService } from "@/gen/broker_pb";

const transport = createConnectTransport({
  baseUrl: window.location.origin,
});

const client = createClient(BrokerService, transport);

const MAX_LOG_LINES = 5000;

interface LogLine {
  timestamp: string;
  text: string;
}

export function LogViewer({ clusterName, jobId }: { clusterName: string; jobId?: string }) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const appendLine = useCallback((text: string) => {
    setLines((prev) => {
      const next = [
        ...prev,
        {
          timestamp: new Date().toISOString().substring(11, 23),
          text,
        },
      ];
      if (next.length > MAX_LOG_LINES) {
        return next.slice(next.length - MAX_LOG_LINES);
      }
      return next;
    });
  }, []);

  useEffect(() => {
    let cancelled = false;
    let retryTimeout: ReturnType<typeof setTimeout>;
    let retryCount = 0;

    async function stream() {
      while (!cancelled) {
        setConnected(true);
        setError(null);
        try {
          const response = client.logs({
            clusterName,
            jobId: jobId ?? "",
            follow: true,
          });

          retryCount = 0;
          for await (const msg of response) {
            if (cancelled) return;
            appendLine(msg.line);
          }
        } catch (err) {
          if (cancelled) return;
          setError(err instanceof Error ? err.message : "stream error");
        } finally {
          if (!cancelled) setConnected(false);
        }

        if (cancelled) return;

        const delay = Math.min(1000 * Math.pow(2, retryCount), 30000);
        retryCount++;
        await new Promise<void>((resolve) => {
          retryTimeout = setTimeout(resolve, delay);
        });
      }
    }

    stream();
    return () => {
      cancelled = true;
      clearTimeout(retryTimeout);
    };
  }, [clusterName, jobId, appendLine]);

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
          {connected ? "streaming" : error ? `reconnecting: ${error}` : "disconnected"}
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
