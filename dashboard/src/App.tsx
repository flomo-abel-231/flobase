import { useEffect, useState, type ReactNode } from "react";
import { api, getToken, refreshSession, setToken, signOut as endSession, type Me, type ServerConfig } from "./api";
import { Logo } from "./ui";
import { AcceptInvite, Login } from "./pages/Login";
import { Projects } from "./pages/Projects";
import { NewProject } from "./pages/NewProject";
import { ProjectDetail } from "./pages/ProjectDetail";
import { OffsiteBackups } from "./pages/OffsiteBackups";
import { Settings } from "./pages/Settings";
import { Assistant } from "./pages/Assistant";
import { AssistantSettings } from "./pages/AssistantSettings";
import { Automations } from "./pages/Automations";
import { Team } from "./pages/Team";
import { Account } from "./pages/Account";

// Hash routes keep the dashboard servable as static files from any host.
type Route =
  | { page: "projects" }
  | { page: "new" }
  | { page: "offsite" }
  | { page: "settings" }
  | { page: "assistant"; chat?: string }
  | { page: "assistant-settings" }
  | { page: "automations" }
  | { page: "team" }
  | { page: "account" }
  | { page: "invite"; token: string }
  | { page: "project"; ref: string; tab: string; section?: string };

function parseRoute(hash: string): Route {
  const path = hash.replace(/^#/, "");
  const project = path.match(/^\/project\/([a-z]+)(?:\/([a-z]+))?(?:\/([a-z]+))?$/);
  if (project) return { page: "project", ref: project[1], tab: project[2] ?? "configuration", section: project[3] };
  if (path === "/new") return { page: "new" };
  if (path === "/offsite") return { page: "offsite" };
  if (path === "/settings") return { page: "settings" };
  if (path === "/assistant/settings") return { page: "assistant-settings" };
  const chat = path.match(/^\/assistant(?:\/([0-9a-f-]{36}))?$/);
  if (chat) return { page: "assistant", chat: chat[1] };
  if (path === "/automations") return { page: "automations" };
  if (path === "/team") return { page: "team" };
  if (path === "/account") return { page: "account" };
  const invite = path.match(/^\/invite\/([A-Za-z0-9]+)$/);
  if (invite) return { page: "invite", token: invite[1] };
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
  assistant: <path d="M10 2.5l1.6 4.4 4.4 1.6-4.4 1.6L10 14.5l-1.6-4.4L4 8.5l4.4-1.6zM15.5 13l.7 1.8 1.8.7-1.8.7-.7 1.8-.7-1.8-1.8-.7 1.8-.7z" />,
  automations: (
    <>
      <circle cx="10" cy="10" r="7" />
      <path d="M10 6v4l2.5 2.5" />
    </>
  ),
  team: (
    <>
      <circle cx="7.5" cy="7" r="2.5" />
      <path d="M3 16a4.5 4.5 0 0 1 9 0M13 4.8a2.5 2.5 0 0 1 0 4.4M14.5 11.5A4.5 4.5 0 0 1 17 16" />
    </>
  ),
  account: (
    <>
      <circle cx="10" cy="7" r="3" />
      <path d="M4 17a6 6 0 0 1 12 0" />
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
  // null = still checking; false = signed out.
  const [me, setMe] = useState<Me | false | null>(null);
  const [setupRequired, setSetupRequired] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);

  useEffect(() => {
    const onHash = () => {
      setRoute(parseRoute(window.location.hash));
      setMenuOpen(false);
    };
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  // load finds out who is signed in: the API token from the CLI or desktop
  // link, else a session the refresh cookie can renew.
  const load = async () => {
    try {
      if (getToken() || (await refreshSession())) {
        setMe(await api.me());
        return;
      }
    } catch {
      setToken("");
    }
    const status = await api.authStatus().catch(() => ({ setup_required: false }));
    setSetupRequired(status.setup_required);
    setMe(false);
  };

  useEffect(() => {
    api.config().then(setConfig).catch(() => setConfig(null));
    void load();
  }, []);

  const signOut = async () => {
    await endSession();
    setMenuOpen(false);
    await load();
  };

  if (me === null) return null;
  if (route.page === "invite") return <AcceptInvite token={route.token} onSignedIn={() => void load()} />;
  if (!me) return <Login setupRequired={setupRequired} onSignedIn={() => void load()} />;

  const section =
    route.page === "project" || route.page === "new" ? "projects" : route.page === "assistant-settings" ? "assistant" : route.page;

  const sidebar = (
    <nav aria-label="Main" className="flex h-full flex-col gap-6 px-3 py-4">
      {/* The phone header already shows the logo above the open menu. */}
      <a href="#/" className="hidden px-1 lg:block">
        <Logo />
      </a>
      <div className="flex flex-col gap-0.5">
        <NavLink href="#/" icon="projects" label="Projects" active={section === "projects"} />
        <NavLink href="#/assistant" icon="assistant" label="Assistant" active={section === "assistant"} />
        <NavLink href="#/automations" icon="automations" label="Automations" active={section === "automations"} />
        <NavLink href="#/offsite" icon="offsite" label="Off-site backups" active={section === "offsite"} />
        <NavLink href="#/team" icon="team" label="Team" active={section === "team"} />
        <NavLink href="#/settings" icon="settings" label="Settings" active={section === "settings"} />
      </div>
      <div className="mt-auto flex flex-col gap-2">
        {config && (
          <span className="px-2.5 label text-ink-muted">
            {config.mode === "cloud" ? "Cloud" : "Self-hosted"} · v{config.version}
          </span>
        )}
        <a
          href="#/account"
          aria-current={section === "account" ? "page" : undefined}
          className={`flex min-w-0 items-center gap-2.5 rounded-sm px-2.5 py-1.5 text-[13px] ${
            section === "account" ? "bg-brand-subtle text-ink" : "text-ink-muted hover:bg-surface-sunken hover:text-ink"
          }`}
        >
          <Icon name="account" />
          <span className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-ink">{me.user ? me.user.name || me.user.email : "API token"}</span>
            {me.user?.name && <span className="truncate text-[12px]">{me.user.email}</span>}
          </span>
        </a>
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
        {route.page === "assistant" && <Assistant chatId={route.chat} />}
        {route.page === "assistant-settings" && <AssistantSettings />}
        {route.page === "automations" && <Automations />}
        {route.page === "team" && <Team me={me} />}
        {route.page === "account" && <Account me={me} onChanged={(user) => setMe({ ...me, user })} onSignOut={signOut} />}
        {route.page === "project" && (
          <ProjectDetail key={route.ref} projectRef={route.ref} tab={route.tab} section={route.section} />
        )}
      </main>
    </div>
  );
}
