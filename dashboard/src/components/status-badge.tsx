import { cn } from "@/lib/cn";

const styles: Record<string, string> = {
  INIT: "bg-yellow-900/50 text-yellow-400 border-yellow-800",
  UP: "bg-green-900/50 text-green-400 border-green-800",
  STOPPED: "bg-neutral-800 text-neutral-400 border-neutral-700",
  RUNNING: "bg-blue-900/50 text-blue-400 border-blue-800",
  PENDING: "bg-yellow-900/50 text-yellow-400 border-yellow-800",
  SUCCEEDED: "bg-green-900/50 text-green-400 border-green-800",
  FAILED: "bg-red-900/50 text-red-400 border-red-800",
  CANCELLED: "bg-neutral-800 text-neutral-400 border-neutral-700",
};

export function StatusBadge({ status }: { status: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium",
        styles[status] ?? "bg-neutral-800 text-neutral-400 border-neutral-700"
      )}
    >
      {status}
    </span>
  );
}
