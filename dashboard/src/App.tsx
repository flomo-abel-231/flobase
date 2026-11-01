import { useEffect, useState, type ReactNode } from "react";
import {
  api,
  ApiError,
  getToken,
  getWorkspace,
  refreshSession,
  setToken,
  setWorkspace,
  signOut as endSession,
  type Me,
  type ServerConfig,
} from "./api";
import { Logo, ThemeSwitch } from "./ui";
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
import { Regions } from "./pages/Regions";
import { WorkspacePage } from "./pages/WorkspacePage";
import { Support } from "./pages/Support";

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
  | { page: "regions" }
  | { page: "workspace" }
  | { page: "support" }
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
  if (path === "/regions") return { page: "regions" };
  if (path === "/workspace") return { page: "workspace" };
  if (path === "/support") return { page: "support" };
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
  heart: <path d="M10 16.5s-6.5-3.9-6.5-8.4A3.6 3.6 0 0 1 10 6a3.6 3.6 0 0 1 6.5 2.1c0 4.5-6.5 8.4-6.5 8.4z" />,
  workspace: (
    <>
      <rect x="3" y="4" width="14" height="12" rx="1.5" />
      <path d="M3 8h14M7 4v4" />
    </>
  ),
  regions: (
    <>
      <circle cx="10" cy="10" r="7" />
      <path d="M3 10h14M10 3c2 2.2 2.8 4.5 2.8 7s-.8 4.8-2.8 7c-2-2.2-2.8-4.5-2.8-7S8 5.2 10 3z" />
    </>
  ),
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

function NavSection({ children }: { children: ReactNode }) {
  return <span className="mt-3 px-2.5 pb-1 text-[11px] font-medium tracking-wide text-ink-muted uppercase first:mt-0">{children}</span>;
}

function NavLink({ href, icon, label, active, onClick }: { href: string; icon: string; label: string; active: boolean; onClick?: () => void }) {
  return (
    <a
      href={href}
      onClick={onClick}
      aria-current={active ? "page" : undefined}
      className={`flex h-8 items-center gap-2.5 rounded-lg px-2.5 text-[13px] font-medium transition ${
        active ? "bg-ink/8 text-ink" : "text-ink-muted hover:bg-ink/5 hover:text-ink"
      }`}
    >
      <span className={active ? "text-brand" : ""}>
        <Icon name={icon} />
      </span>
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
  const [signup, setSignup] = useState(false);
  const [ownerEmail, setOwnerEmail] = useState(false);
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
        // A remembered workspace the person is no longer in (removed, or
        // deleted) is refused: forget it rather than signing them out.
        let m = await api.me().catch((err) => {
          if (err instanceof ApiError && err.status === 403 && getWorkspace()) {
            setWorkspace("");
            return api.me();
          }
          throw err;
        });
        // Remember the workspace the server settled on, unless the one
        // remembered is still valid.
        if (!m.workspaces.some((w) => w.id === getWorkspace())) {
          setWorkspace(m.workspace || (m.workspaces[0]?.id ?? ""));
          if (m.workspace !== getWorkspace()) m = await api.me();
        }
        setMe(m);
        return;
      }
    } catch {
      setToken("");
    }
    const status = await api.authStatus().catch(() => ({ setup_required: false, signup: false, owner_email: false }));
    setSetupRequired(status.setup_required);
    setSignup(status.signup);
    setOwnerEmail(status.owner_email);
    setMe(false);
  };

  useEffect(() => {
    api
      .config()
      .then(setConfig)
      .catch(() => setConfig(null));
    void load();
  }, []);

  const signOut = async () => {
    await endSession();
    setWorkspace("");
    setMenuOpen(false);
    await load();
  };

  if (me === null) return null;
  if (route.page === "invite") return <AcceptInvite token={route.token} onSignedIn={() => void load()} />;
  if (!me) return <Login setupRequired={setupRequired} signup={signup} ownerEmail={ownerEmail} onSignedIn={() => void load()} />;

  // Switching workspace re-reads who we are there and remounts every page,
  // so nothing from the previous workspace stays on screen.
  const switchWorkspace = async (id?: string) => {
    if (id !== undefined) {
      setWorkspace(id);
      navigate("/");
    }
    await load();
  };
  const platformAdmin = me.role === "owner" || me.role === "admin";
  const current = me.workspaces.find((w) => w.id === me.workspace);
  // Donations are for the open-source, self-hosted FloBase; the hosted
  // cloud doesn't ask for them.
  const supportPage = config?.mode === "self-hosted";

  const section =
    route.page === "project" || route.page === "new"
      ? "projects"
      : route.page === "assistant-settings"
        ? "assistant"
        : route.page;

  const workspaceSwitch = (
    <select
      aria-label="Workspace"
      value={me.workspace}
      onChange={(e) => (e.target.value === "+" ? navigate("/workspace") : void switchWorkspace(e.target.value))}
      className="h-8 min-w-0 max-w-56 rounded-lg border border-border bg-surface-raised px-2 text-[13px] font-medium text-ink hover:border-border-strong"
    >
      {me.workspaces.length === 0 && <option value="">No workspace</option>}
      {me.workspaces.map((w) => (
        <option key={w.id} value={w.id}>
          {w.name}
        </option>
      ))}
      {me.user && <option value="+">+ New workspace…</option>}
    </select>
  );

  // The menu follows Coolify's sections; Infrastructure is for the people
  // who run this FloBase.
  const sidebar = (
    <nav aria-label="Main" className="flex h-full flex-col px-3 pt-3 pb-3">
      <div className="mb-3 lg:hidden">{workspaceSwitch}</div>
      <div className="flex flex-col gap-0.5 overflow-y-auto">
        <NavSection>Workspace</NavSection>
        <NavLink href="#/" icon="projects" label="Projects" active={section === "projects"} />
        <NavLink href="#/assistant" icon="assistant" label="Assistant" active={section === "assistant"} />
        <NavLink href="#/automations" icon="automations" label="Automations" active={section === "automations"} />
        <NavSection>Manage</NavSection>
        <NavLink href="#/team" icon="team" label="Members" active={section === "team"} />
        <NavLink href="#/workspace" icon="workspace" label="Workspace" active={section === "workspace"} />
        <NavLink href="#/settings" icon="settings" label="Settings" active={section === "settings"} />
        {platformAdmin && (
          <>
            <NavSection>Infrastructure</NavSection>
            <NavLink href="#/regions" icon="regions" label="Regions" active={section === "regions"} />
            <NavLink href="#/offsite" icon="offsite" label="Off-site backups" active={section === "offsite"} />
          </>
        )}
      </div>
      <div className="mt-auto flex flex-col gap-0.5 border-t border-border pt-3">
        {supportPage && (
          <a
            href="#/support"
            aria-current={section === "support" ? "page" : undefined}
            className={`mb-2 flex items-center gap-2.5 rounded-lg border px-2.5 py-2 text-[13px] font-medium transition ${
              section === "support" ? "border-brand bg-brand-subtle text-ink" : "border-border bg-surface-raised text-ink hover:border-border-strong"
            }`}
          >
            <span className="text-brand">
              <Icon name="heart" />
            </span>
            <span className="flex flex-col leading-4">
              Support FloBase
              <span className="text-[11px] font-normal text-ink-muted">Free and open source</span>
            </span>
          </a>
        )}
        <a
          href="#/account"
          aria-current={section === "account" ? "page" : undefined}
          className={`flex min-w-0 items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-[13px] transition ${
            section === "account" ? "bg-ink/8 text-ink" : "text-ink-muted hover:bg-ink/5 hover:text-ink"
          }`}
        >
          <Icon name="account" />
          <span className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-ink">{me.user ? me.user.name || me.user.email : "API token"}</span>
            {me.user?.name && <span className="truncate text-[11px]">{me.user.email}</span>}
          </span>
        </a>
        <button
          type="button"
          onClick={signOut}
          className="flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[13px] font-medium text-ink-muted transition hover:bg-ink/5 hover:text-ink"
        >
          <Icon name="logout" />
          Sign out
        </button>
      </div>
    </nav>
  );

  return (
    <div className="min-h-screen bg-surface">
      {/* Desktop: a fixed top bar with the brand cell over the sidebar. */}
      <header className="fixed inset-x-0 top-0 z-30 hidden h-12 items-center border-b border-border bg-surface-panel/95 backdrop-blur lg:flex">
        <a href="#/" className="flex h-full w-56 shrink-0 items-center border-r border-border px-4">
          <Logo version={config?.version} />
        </a>
        <div className="flex min-w-0 flex-1 items-center justify-between gap-3 px-6">
          {workspaceSwitch}
          <div className="flex items-center gap-2">
            <a
              href="https://github.com/flomo-abel-231/flobase"
              target="_blank"
              rel="noreferrer"
              className="rounded-lg px-2.5 py-1.5 text-[13px] text-ink-muted transition hover:bg-ink/5 hover:text-ink"
            >
              GitHub
            </a>
            <ThemeSwitch />
          </div>
        </div>
      </header>
      <aside className="fixed top-12 bottom-0 left-0 z-20 hidden w-56 border-r border-border bg-surface-panel lg:block">{sidebar}</aside>

      {/* Phone and tablet: a sticky bar and a drawer from the right. */}
      <header className="sticky top-0 z-30 flex h-12 items-center justify-between gap-3 border-b border-border bg-surface-panel/95 px-4 backdrop-blur lg:hidden">
        <a href="#/">
          <Logo />
        </a>
        <button
          type="button"
          onClick={() => setMenuOpen(!menuOpen)}
          aria-expanded={menuOpen}
          aria-controls="mobile-menu"
          className="inline-flex size-9 items-center justify-center rounded-lg border border-border text-ink"
        >
          <span className="sr-only">Menu</span>
          <svg viewBox="0 0 20 20" className="size-4" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden>
            <path d={menuOpen ? "M5 5l10 10M15 5 5 15" : "M3 6h14M3 10h14M3 14h14"} />
          </svg>
        </button>
      </header>
      {menuOpen && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <button type="button" aria-label="Close menu" onClick={() => setMenuOpen(false)} className="absolute inset-0 bg-black/50" />
          <div id="mobile-menu" className="absolute inset-y-0 right-0 w-72 max-w-[85vw] rounded-l-2xl border-l border-border bg-surface-panel">
            {sidebar}
          </div>
        </div>
      )}

      <div className="lg:pt-12 lg:pl-56">
      <main key={me.workspace} className="mx-auto max-w-[1400px] px-5 py-6 sm:px-8 lg:px-10 lg:pt-7 lg:pb-10">
        {!current && route.page !== "account" && route.page !== "support" && <WorkspacePage me={me} onChanged={switchWorkspace} />}
        {(current || route.page === "account" || route.page === "support") && (
          <Pages
            route={route}
            me={me}
            config={config}
            platformAdmin={platformAdmin}
            onMe={setMe}
            onSignOut={signOut}
            onWorkspace={switchWorkspace}
          />
        )}
      </main>
      </div>
    </div>
  );
}

function Pages({
  route,
  me,
  config,
  platformAdmin,
  onMe,
  onSignOut,
  onWorkspace,
}: {
  route: Route;
  me: Me;
  config: ServerConfig | null;
  platformAdmin: boolean;
  onMe: (me: Me) => void;
  onSignOut: () => Promise<void>;
  onWorkspace: (id?: string) => Promise<void>;
}) {
  const signOut = onSignOut;
  const setMe = onMe;
  return (
    <>
      {route.page === "projects" && <Projects />}
      {route.page === "new" && <NewProject />}
      {route.page === "offsite" && platformAdmin && <OffsiteBackups />}
      {route.page === "settings" && <Settings config={config} platformAdmin={platformAdmin} />}
      {route.page === "assistant" && <Assistant chatId={route.chat} />}
      {route.page === "assistant-settings" && <AssistantSettings />}
      {route.page === "automations" && <Automations />}
      {route.page === "team" && <Team me={me} />}
      {route.page === "regions" && platformAdmin && <Regions me={me} />}
      {route.page === "workspace" && <WorkspacePage me={me} onChanged={onWorkspace} />}
      {route.page === "support" && config?.mode === "self-hosted" && <Support />}
      {route.page === "account" && <Account me={me} onChanged={(user) => setMe({ ...me, user })} onSignOut={signOut} />}
      {route.page === "project" && (
        <ProjectDetail key={route.ref} projectRef={route.ref} tab={route.tab} section={route.section} />
      )}
    </>
  );
}
