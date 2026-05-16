import { Outlet, NavLink } from "react-router";
import { ArrowLeft, ListChecks, Settings as SettingsIcon } from "lucide-react";
import { useEffect, useState } from "react";

import { cn } from "@/lib/utils";
import { loadIdentity, currentUser } from "@/lib/identity";

// Plain anchor (not router Link) so the browser does a full-page nav out of
// the plugin proxy back to continuum's admin section.
const backToContinuumHref = "/admin/plugins";

export default function Layout() {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    loadIdentity()
      .then(() => setReady(true))
      .catch(() => setReady(true));
  }, []);

  if (!ready) return null;
  const user = currentUser();
  const embedded = new URLSearchParams(window.location.search).get("embedded") === "1";

  if (!user?.isAdmin) {
    return (
      <div className="bg-background text-foreground min-h-screen p-12 text-center">
        <h1 className="text-xl font-semibold">Admin access required</h1>
        <p className="text-muted-foreground mt-2">
          You need an admin role to view this page.
        </p>
        <a href={backToContinuumHref} className="mt-4 inline-block underline">
          ← Back to Continuum
        </a>
      </div>
    );
  }

  return (
    <div className="bg-background relative min-h-[100dvh] overflow-x-hidden">
      {!embedded && (
        <>
          <div className="from-primary/6 pointer-events-none fixed inset-x-0 top-0 z-0 h-40 bg-gradient-to-b to-transparent blur-3xl" />

          <header className="glass-dark border-border/70 sticky top-0 z-30 mx-3 mt-3 flex items-center justify-between rounded-2xl border px-4 py-3 sm:mx-6 lg:mx-8">
            <div className="flex items-center gap-3">
              <a
                href={backToContinuumHref}
                className="text-muted-foreground hover:bg-surface-hover hover:text-foreground inline-flex items-center gap-1.5 rounded-lg px-2 py-1.5 text-xs font-medium transition-colors"
                title="Back to Continuum plugins"
              >
                <ArrowLeft className="size-4" />
                <span className="hidden sm:inline">Continuum</span>
              </a>
              <span className="text-border/60" aria-hidden>
                /
              </span>
              <h1 className="text-base font-semibold tracking-tight">WHMCS Login</h1>
            </div>
            <PluginNav />
          </header>
        </>
      )}

      {embedded && (
        <header className="border-border/70 bg-background/95 sticky top-0 z-30 border-b px-5 py-3 backdrop-blur">
          <PluginNav />
        </header>
      )}

      <main
        id="main-content"
        className={
          embedded
            ? "relative z-10 px-5 py-5"
            : "relative z-10 mx-auto max-w-5xl px-4 py-6 sm:px-6 lg:px-8"
        }
      >
        <Outlet />
      </main>
    </div>
  );
}

function PluginNav() {
  return (
    <nav className="flex items-center gap-1">
      <NavTab to="/products" icon={<ListChecks className="size-4" />}>
        Products
      </NavTab>
      <NavTab to="/settings" icon={<SettingsIcon className="size-4" />}>
        Settings
      </NavTab>
    </nav>
  );
}

function NavTab({
  to,
  icon,
  children,
}: {
  to: string;
  icon: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        cn(
          "inline-flex items-center gap-2 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
          isActive
            ? "bg-surface text-foreground"
            : "text-muted-foreground hover:bg-surface-hover hover:text-foreground",
        )
      }
    >
      {icon}
      {children}
    </NavLink>
  );
}
