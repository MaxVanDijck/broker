import { Link, Outlet, useLocation } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import { Server, Activity, Terminal } from "lucide-react";

const nav = [
  { to: "/", label: "Clusters", icon: Server },
  { to: "/jobs", label: "Jobs", icon: Activity },
] as const;

export function Layout() {
  const location = useLocation();

  return (
    <div className="min-h-screen bg-neutral-950 text-neutral-100">
      <header className="border-b border-neutral-800">
        <div className="mx-auto flex h-14 max-w-7xl items-center gap-8 px-6">
          <Link to="/" className="flex items-center gap-2 font-semibold">
            <Terminal className="h-5 w-5" />
            <span>broker</span>
          </Link>
          <nav className="flex gap-1">
            {nav.map(({ to, label, icon: Icon }) => (
              <Link
                key={to}
                to={to}
                className={cn(
                  "flex items-center gap-2 rounded-md px-3 py-1.5 text-sm transition-colors",
                  location.pathname === to
                    ? "bg-neutral-800 text-white"
                    : "text-neutral-400 hover:text-neutral-200"
                )}
              >
                <Icon className="h-4 w-4" />
                {label}
              </Link>
            ))}
          </nav>
        </div>
      </header>
      <main className="mx-auto max-w-7xl px-6 py-8">
        <Outlet />
      </main>
    </div>
  );
}
