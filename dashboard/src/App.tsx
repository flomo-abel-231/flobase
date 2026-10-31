import { useEffect, useState, type ReactNode } from "react";
import { api, ApiError, getToken, setToken, type ServerConfig } from "./api";
import { Logo } from "./ui";
import { Login } from "./pages/Login";
import { Projects } from "./pages/Projects";
import { NewProject } from "./pages/NewProject";
import { ProjectDetail } from "./pages/ProjectDetail";
import { OffsiteBackups } from "./pages/OffsiteBackups";
import { Settings } from "./pages/Settings";

// Hash routes keep the dashboard servable as static files from any host.
type Route =
  | { page: "projects" }
  | { page: "new" }
  | { page: "offsite" }
  | { page: "settings" }
  | { page: "project"; ref: string; tab: string; section?: string };

function parseRoute(hash: string): Route {
  const path = hash.replace(/^#/, "");
  const project = path.match(/^\/project\/([a-z]+)(?:\/([a-z]+))?(?:\/([a-z]+))?$/);
  if (project) return { page: "project", ref: project[1], tab: project[2] ?? "configuration", section: project[3] };
  if (path === "/new") return { page: "new" };
  if (path === "/offsite") return { page: "offsite" };
  if (path === "/settings") return { page: "settings" };
  return { page: "projects" };
}

export function navigate(path: string) {
  window.location.hash = path;
}

const icons: Record<string, ReactNode> = {
  projects: <path d="M3 5.5A1.5 1.5 0 0 1 4.5 4h3.6l1.5 2h5.9A1.5 1.5 0 0 1 17 7.5v7a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 3 14.5z" />,
  offsite: <path d="M6 15.5h8.5a3 3 0 0 0 .4-6 4.5 4.5 0 0 0-8.7-1A3.5 3.5 0 0 0 6 15.5zM10 9v5M8 12l2 2 2-2" />,
  settings: (
    <>
      <circle cx="10" cy="10" r="2.5" />
      <path d="M10 2.5v2M10 15.5v2M2.5 10h2M15.5 10h2M4.7 4.7l1.4 1.4M13.9 13.9l1.4 1.4M4.7 15.3l1.4-1.4M13.9 6.1l1.4-1.4" />
    </>
  ),
  logout: <path d="M8 4H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3M12 6.5 15.5 10 12 13.5M15.5 10H8" />,
};

function Icon({ name }: { name: string }) {
  return (
    <svg viewBox="0 0 20 20" className="size-4 shrink-0" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      {icons[name]}
    </svg>
  );
}

function NavLink({ href, icon, label, active, onClick }: { href: string; icon: string; label: string; active: boolean; onClick?: () => void }) {
  return (
    <a
      href={href}
      onClick={onClick}
      aria-current={active ? "page" : undefined}
      className={`flex items-center gap-2.5 rounded-sm px-2.5 py-1.5 text-[13px] font-medium transition ${
        active ? "bg-brand-subtle text-ink" : "text-ink-muted hover:bg-surface-sunken hover:text-ink"
      }`}
    >
      <Icon name={icon} />
      {label}
    </a>
  );
}

export function App() {
  const [route, setRoute] = useState<Route>(() => parseRoute(window.location.hash));
  const [config, setConfig] = useState<ServerConfig | null>(null);
  const [authed, setAuthed] = useState<boolean | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);

  useEffect(() => {
    const onHash = () => {
      setRoute(parseRoute(window.location.hash));
      setMenuOpen(false);
    };
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  useEffect(() => {
    api.config().then(setConfig).catch(() => setConfig(null));
    if (!getToken()) {
      setAuthed(false);
      return;
    }
    api
      .me()
      .then(() => setAuthed(true))
      .catch((e) => setAuthed(!(e instanceof ApiError && e.status === 401)));
  }, []);

  const signOut = () => {
    setToken("");
    setAuthed(false);
  };

  if (authed === null) return null;
  if (!authed) return <Login onSignedIn={() => setAuthed(true)} />;

  const section = route.page === "project" || route.page === "new" ? "projects" : route.page;

  const sidebar = (
    <nav aria-label="Main" className="flex h-full flex-col gap-6 px-3 py-4">
      {/* The phone header already shows the logo above the open menu. */}
      <a href="#/" className="hidden px-1 lg:block">
        <Logo />
      </a>
      <div className="flex flex-col gap-0.5">
        <NavLink href="#/" icon="projects" label="Projects" active={section === "projects"} />
        <NavLink href="#/offsite" icon="offsite" label="Off-site backups" active={section === "offsite"} />
        <NavLink href="#/settings" icon="settings" label="Settings" active={section === "settings"} />
      </div>
      <div className="mt-auto flex flex-col gap-2">
        {config && (
          <span className="px-2.5 label text-ink-muted">
            {config.mode === "cloud" ? "Cloud" : "Self-hosted"} · v{config.version}
          </span>
        )}
        <button
          type="button"
          onClick={signOut}
          className="flex items-center gap-2.5 rounded-sm px-2.5 py-1.5 text-left text-[13px] font-medium text-ink-muted hover:bg-surface-sunken hover:text-ink"
        >
          <Icon name="logout" />
          Sign out
        </button>
      </div>
    </nav>
  );

  return (
    <div className="min-h-screen lg:pl-56">
      <aside className="fixed inset-y-0 left-0 hidden w-56 border-r border-border bg-surface-raised lg:block">{sidebar}</aside>

      <header className="sticky top-0 z-20 flex h-12 items-center justify-between border-b border-border bg-surface-raised px-4 lg:hidden">
        <a href="#/">
          <Logo />
        </a>
        <button
          type="button"
          onClick={() => setMenuOpen(!menuOpen)}
          aria-expanded={menuOpen}
          aria-controls="mobile-menu"
          className="inline-flex size-8 items-center justify-center rounded-sm border border-border-strong text-ink"
        >
          <span className="sr-only">Menu</span>
          <svg viewBox="0 0 20 20" className="size-4" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden>
            <path d={menuOpen ? "M5 5l10 10M15 5 5 15" : "M3 6h14M3 10h14M3 14h14"} />
          </svg>
        </button>
      </header>
      {menuOpen && (
        <div id="mobile-menu" className="fixed inset-x-0 top-12 bottom-0 z-20 bg-surface-raised lg:hidden">
          {sidebar}
        </div>
      )}

      <main className="mx-auto max-w-6xl px-4 py-6 sm:px-8 sm:py-8">
        {route.page === "projects" && <Projects />}
        {route.page === "new" && <NewProject />}
        {route.page === "offsite" && <OffsiteBackups />}
        {route.page === "settings" && <Settings config={config} />}
        {route.page === "project" && (
          <ProjectDetail key={route.ref} projectRef={route.ref} tab={route.tab} section={route.section} />
        )}
      </main>
    </div>
  );
}
