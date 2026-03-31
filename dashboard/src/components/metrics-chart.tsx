import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from "recharts";

const NODE_COLORS = [
  "#3b82f6", // blue
  "#10b981", // emerald
  "#f59e0b", // amber
  "#ef4444", // red
  "#8b5cf6", // violet
  "#ec4899", // pink
  "#06b6d4", // cyan
  "#f97316", // orange
];

interface MetricPoint {
  timestamp: string;
  node_id: string;
  cpu_percent: number;
  memory_percent: number;
  gpu_utilization: number;
  gpu_memory_used: number;
}

interface MetricsChartProps {
  title: string;
  points: MetricPoint[];
  nodeIds: string[];
  dataKey: keyof MetricPoint;
  unit?: string;
  formatValue?: (value: number) => string;
}

function formatTime(ts: string): string {
  const d = new Date(ts);
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export function MetricsChart({
  title,
  points,
  nodeIds,
  dataKey,
  unit = "",
  formatValue,
}: MetricsChartProps) {
  // pivot: group by timestamp, one key per node
  const grouped = new Map<string, Record<string, number | string>>();
  for (const p of points) {
    let row = grouped.get(p.timestamp);
    if (!row) {
      row = { timestamp: p.timestamp };
      grouped.set(p.timestamp, row);
    }
    row[p.node_id] = p[dataKey] as number;
  }

  const data = Array.from(grouped.values()).sort((a, b) =>
    (a.timestamp as string).localeCompare(b.timestamp as string),
  );

  if (data.length === 0) {
    return (
      <div className="rounded-lg border border-neutral-800 bg-neutral-900/30 p-4">
        <h3 className="mb-3 text-sm font-medium text-neutral-400">{title}</h3>
        <div className="flex h-48 items-center justify-center text-xs text-neutral-600">
          No data
        </div>
      </div>
    );
  }

  return (
    <div className="rounded-lg border border-neutral-800 bg-neutral-900/30 p-4">
      <h3 className="mb-3 text-sm font-medium text-neutral-400">{title}</h3>
      <ResponsiveContainer width="100%" height={220}>
        <LineChart data={data} margin={{ top: 4, right: 12, bottom: 4, left: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#262626" />
          <XAxis
            dataKey="timestamp"
            tickFormatter={formatTime}
            stroke="#525252"
            tick={{ fill: "#737373", fontSize: 11 }}
            tickLine={false}
          />
          <YAxis
            stroke="#525252"
            tick={{ fill: "#737373", fontSize: 11 }}
            tickLine={false}
            tickFormatter={(v: number) => (formatValue ? formatValue(v) : `${v}${unit}`)}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: "#171717",
              border: "1px solid #262626",
              borderRadius: 8,
              fontSize: 12,
            }}
            labelStyle={{ color: "#a3a3a3" }}
            labelFormatter={(label) => formatTime(String(label))}
            formatter={(value) => {
              const num = Number(value);
              return formatValue ? formatValue(num) : `${num.toFixed(1)}${unit}`;
            }}
          />
          <Legend
            wrapperStyle={{ fontSize: 11, color: "#a3a3a3" }}
          />
          {nodeIds.map((nodeId, i) => (
            <Line
              key={nodeId}
              type="monotone"
              dataKey={nodeId}
              name={nodeId}
              stroke={NODE_COLORS[i % NODE_COLORS.length]}
              strokeWidth={1.5}
              dot={false}
              connectNulls
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
