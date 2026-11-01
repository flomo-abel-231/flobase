// Typed client for the control-plane API.

export type EngineId = "postgres" | "mysql" | "mongodb";
export type Status = "provisioning" | "active" | "failed" | "deleting";

export interface Engine {
  id: EngineId;
  name: string;
  description: string;
  licence: string;
}

export interface Plan {
  id: string;
  name: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  files_gb: number;
  egress_gb: number;
  backups: string;
  est_cost_usd: number;
  price_usd: number;
  markup?: number;
  description: string;
}

export interface Region {
  id: string;
  name: string;
  provider: "docker" | "aws" | "gcp" | "azure";
  location: string;
  status: "open" | "full" | "closed";
  price_multiplier: number;
}

export interface Catalog {
  engines: Engine[];
  plans: Plan[];
  regions: Region[];
  billing: boolean;
  target_markup: number;
}

export interface Project {
  id: string;
  ref: string;
  name: string;
  engine: EngineId;
  region: string;
  plan: string;
  status: Status;
  status_detail: string;
  created_at: string;
  updated_at: string;
  connection?: {
    host: string;
    port: number;
    user: string;
    password: string;
    database: string;
    uri: string;
    pooler_uri?: string;
  };
  keys?: { anon: string; service_role: string };
  api_url?: string;
}

export interface QueryResult {
  columns: string[];
  rows: (string | null)[][];
  command: string;
  truncated?: boolean;
}

export interface QueryResponse {
  results: QueryResult[];
  duration_ms: number;
}

export interface TableInfo {
  name: string;
  rls: boolean;
  row_estimate: number;
}

export interface Column {
  name: string;
  type: string;
  nullable: boolean;
  default: string | null;
  primary: boolean;
}

export interface TableRows {
  columns: Column[];
  rows: Record<string, unknown>[];
  total: number;
  rls: boolean;
}

export interface AuthUser {
  id: string;
  email: string | null;
  created_at: string;
  last_sign_in_at: string | null;
  confirmed_at: string | null;
}

export interface Backup {
  name: string;
  finished: string;
  size_bytes: number;
}

export interface Backups {
  enabled: boolean;
  point_in_time: boolean;
  retention_days: number;
  backups: Backup[];
  window?: { from: string; to: string };
  running: boolean;
  offsite: { enabled: boolean; bucket?: string; encrypted: boolean; last_sync?: string; failing: boolean };
}

export interface OffsiteProject {
  ref: string;
  meta: { name?: string; plan?: string; engine?: string; created_at?: string };
  backups: Backup[];
  last_change?: string;
  here: boolean;
}

export interface OffsiteList {
  enabled: boolean;
  bucket?: string;
  encrypted?: boolean;
  projects: OffsiteProject[];
}

export type RestoreTarget = { latest: true } | { time: string } | { backup: string };

export interface Updates {
  current: string;
  latest?: string;
  available: boolean;
  notes?: string;
  release_url?: string;
  auto: boolean;
  /** "server": install.sh; "local": FloBase Desktop or the flobase CLI. */
  install: "server" | "local";
  can_update: boolean;
  updating: boolean;
  checked_at?: string;
  error?: string;
  manual_steps?: string;
}

export interface StorageLocation {
  id: string;
  name: string;
  provider: string;
  endpoint: string;
  region: string;
  bucket: string;
  prefix: string;
  access_key_hint: string;
  encrypted: boolean;
  active: boolean;
  from_env: boolean;
  created_at?: string;
}

export interface StorageInput {
  name: string;
  provider: string;
  endpoint: string;
  region: string;
  bucket: string;
  prefix: string;
  access_key: string;
  secret_key: string;
  encryption_key: string;
  activate?: boolean;
}

export interface DeleteCheck {
  has_data: boolean;
  summary: string;
  cloud_backup: boolean;
  bucket?: string;
  engine_backup: boolean;
}

export type AssistantMode = "read" | "ask" | "auto";

export interface LLMProvider {
  id: string;
  name: string;
  kind: "anthropic" | "openai";
  preset: string;
  base_url: string;
  model: string;
  api_key_hint: string;
  default: boolean;
  created_at: string;
}

export interface ProviderInput {
  id?: string;
  name: string;
  kind: "anthropic" | "openai";
  preset: string;
  base_url: string;
  api_key: string;
  model: string;
}

export interface ToolEntry {
  call_id: string;
  name: string;
  title: string;
  detail?: string;
  risk: "read" | "change" | "delete";
  status: "running" | "done" | "failed" | "waiting" | "approved" | "denied";
  output?: string;
}

export interface ChatEntry {
  role: "user" | "assistant" | "tool" | "notice";
  text?: string;
  tool?: ToolEntry;
  at: string;
}

export interface Chat {
  id: string;
  title: string;
  provider_id: string | null;
  automation_id: string | null;
  status: "idle" | "running" | "waiting" | "failed";
  entries?: ChatEntry[];
  created_at: string;
  updated_at: string;
}

export interface Automation {
  id: string;
  name: string;
  prompt: string;
  provider_id: string | null;
  every: "hour" | "day" | "week";
  enabled: boolean;
  next_run_at: string;
  last_run_at: string | null;
  last_chat_id: string | null;
  last_status?: Chat["status"];
}

export interface AutomationInput {
  name: string;
  prompt: string;
  provider_id: string;
  every: Automation["every"];
  enabled?: boolean;
}

export interface NewColumn {
  name: string;
  type: string;
  nullable: boolean;
}

export interface ServerConfig {
  mode: "self-hosted" | "cloud";
  version: string;
  billing: boolean;
}

// Two ways to be signed in. A person signs in with email and password and
// gets a session: a short-lived access token kept in memory only, renewed
// from a refresh token the server keeps in an httpOnly cookie. The install's
// API token (from the CLI or desktop app link) is kept for this tab.
const TOKEN_KEY = "flobase.token";

export function getToken(): string {
  try {
    return sessionStorage.getItem(TOKEN_KEY) ?? "";
  } catch {
    return "";
  }
}

export function setToken(token: string) {
  try {
    if (token) sessionStorage.setItem(TOKEN_KEY, token);
    else sessionStorage.removeItem(TOKEN_KEY);
  } catch {
    // Storage can be blocked; the token then lasts for this page only.
  }
  memoryToken = token;
  usingSession = false;
}

let memoryToken = getToken();
let usingSession = false;
let refreshTimer: ReturnType<typeof setTimeout> | undefined;

export interface User {
  id: string;
  email: string;
  name: string;
  role: "owner" | "admin" | "member";
  created_at: string;
  last_sign_in_at: string | null;
}

export interface Me {
  kind: "user" | "token";
  role: User["role"];
  user?: User;
}

export interface Invite {
  id: string;
  email: string;
  role: "admin" | "member";
  created_at: string;
  expires_at: string;
}

interface SessionResponse {
  access_token: string;
  expires_in: number;
  user: User;
}

function applySession(res: SessionResponse) {
  memoryToken = res.access_token;
  usingSession = true;
  try {
    sessionStorage.removeItem(TOKEN_KEY);
  } catch {
    // Nothing stored.
  }
  clearTimeout(refreshTimer);
  // Renew a minute before the access token runs out.
  refreshTimer = setTimeout(() => void refreshSession(), Math.max(10, res.expires_in - 60) * 1000);
}

let refreshing: Promise<boolean> | null = null;

// refreshSession exchanges the refresh cookie for a new access token. Each
// refresh token works once, so tabs take turns: a lock where the browser
// has one, and one request at a time within this tab.
export function refreshSession(): Promise<boolean> {
  if (!refreshing) {
    const run = async () => {
      const res = await fetch("/api/v1/auth/token?grant_type=refresh_token", { method: "POST" });
      if (!res.ok) return false;
      applySession(await res.json());
      return true;
    };
    const locked = navigator.locks ? navigator.locks.request("flobase-refresh", run) : run();
    refreshing = locked.catch(() => false).finally(() => (refreshing = null));
  }
  return refreshing;
}

export async function signOut(scope: "local" | "global" = "local") {
  clearTimeout(refreshTimer);
  if (usingSession) {
    await fetch(`/api/v1/auth/logout?scope=${scope}`, {
      method: "POST",
      headers: { Authorization: `Bearer ${memoryToken}` },
    }).catch(() => undefined);
  }
  setToken("");
}

async function authRequest(path: string, body: unknown): Promise<User> {
  const res = await rawRequest("POST", `/auth/${path}`, body, false);
  const session = res as SessionResponse;
  applySession(session);
  return session.user;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public detail?: { hint?: string; detail?: string; position?: number },
  ) {
    super(message);
  }
}

function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  return rawRequest(method, path, body, true) as Promise<T>;
}

async function rawRequest(method: string, path: string, body: unknown, retry: boolean): Promise<unknown> {
  const res = await fetch(`/api/v1${path}`, {
    method,
    headers: {
      ...(memoryToken ? { Authorization: `Bearer ${memoryToken}` } : {}),
      ...(body ? { "Content-Type": "application/json" } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  // An access token can run out while the tab sleeps: renew it once and
  // try again.
  if (res.status === 401 && retry && usingSession && (await refreshSession())) {
    return rawRequest(method, path, body, false);
  }
  if (!res.ok) {
    let message = `Request failed (${res.status}).`;
    let detail;
    try {
      const body = await res.json();
      message = body.error ?? message;
      detail = body;
    } catch {
      // Not JSON: keep the generic message.
    }
    throw new ApiError(res.status, message, detail);
  }
  if (res.status === 204 || (res.status === 202 && method === "DELETE")) return undefined;
  const text = await res.text();
  return text ? JSON.parse(text) : undefined;
}

export const api = {
  config: () => request<ServerConfig>("GET", "/config"),
  me: () => request<Me>("GET", "/me"),
  authStatus: () => request<{ setup_required: boolean }>("GET", "/auth/status"),
  setupOwner: (input: { api_token: string; email: string; name: string; password: string }) => authRequest("setup", input),
  signIn: (email: string, password: string) => authRequest("token?grant_type=password", { email, password }),
  inviteInfo: (token: string) => request<{ email: string; role: Invite["role"] }>("GET", `/auth/invites/${encodeURIComponent(token)}`),
  acceptInvite: (token: string, name: string, password: string) =>
    authRequest(`invites/${encodeURIComponent(token)}/accept`, { name, password }),
  updateMe: (name: string) => request<User>("PATCH", "/me", { name }),
  changePassword: (current_password: string, new_password: string) =>
    request<void>("POST", "/me/password", { current_password, new_password }),
  team: () => request<{ users: User[]; invites: Invite[] }>("GET", "/team"),
  invite: (email: string, role: Invite["role"]) => request<{ invite: Invite; token: string }>("POST", "/team/invites", { email, role }),
  deleteInvite: (id: string) => request<void>("DELETE", `/team/invites/${id}`),
  setRole: (id: string, role: Invite["role"]) => request<void>("PATCH", `/team/users/${id}`, { role }),
  removeUser: (id: string) => request<void>("DELETE", `/team/users/${id}`),
  catalog: () => request<Catalog>("GET", "/catalog"),
  projects: () => request<Project[]>("GET", "/projects"),
  project: (ref: string) => request<Project>("GET", `/projects/${ref}`),
  createProject: (input: { name: string; engine: EngineId; region: string; plan: string }) =>
    request<Project>("POST", "/projects", input),
  deleteProject: (ref: string, confirm: string) => request<void>("DELETE", `/projects/${ref}`, { confirm }),
  resetPassword: (ref: string) => request<Project>("POST", `/projects/${ref}/password`),
  deleteCheck: (ref: string) => request<DeleteCheck>("GET", `/projects/${ref}/delete-check`),
  backupToCloud: (ref: string) => request<{ bucket: string; backup: string; finished: string }>("POST", `/projects/${ref}/backups/offsite`),
  storages: () => request<{ locations: StorageLocation[]; env_managed: boolean }>("GET", "/storages"),
  testStorage: (input: StorageInput) => request<{ result: string }>("POST", "/storages/test", input),
  createStorage: (input: StorageInput) => request<{ id: string }>("POST", "/storages", input),
  activateStorage: (id: string) => request<void>("POST", `/storages/${id}/activate`),
  deactivateStorage: () => request<void>("POST", "/storages/deactivate"),
  deleteStorage: (id: string) => request<void>("DELETE", `/storages/${id}`),
  assistantSettings: () => request<{ mode: AssistantMode; providers: LLMProvider[] }>("GET", "/assistant/settings"),
  setAssistantMode: (mode: AssistantMode) => request<void>("PUT", "/assistant/settings", { mode }),
  providerModels: (input: Partial<ProviderInput>) => request<{ models: string[] }>("POST", "/assistant/providers/models", input),
  testProvider: (input: ProviderInput) => request<{ result: string }>("POST", "/assistant/providers/test", input),
  createProvider: (input: ProviderInput) => request<LLMProvider>("POST", "/assistant/providers", input),
  updateProvider: (id: string, input: { name: string; model: string; api_key: string }) =>
    request<void>("PATCH", `/assistant/providers/${id}`, input),
  defaultProvider: (id: string) => request<void>("POST", `/assistant/providers/${id}/default`),
  deleteProvider: (id: string) => request<void>("DELETE", `/assistant/providers/${id}`),
  chats: () => request<Chat[]>("GET", "/assistant/chats"),
  chat: (id: string) => request<Chat>("GET", `/assistant/chats/${id}`),
  createChat: (message: string, providerId: string) =>
    request<Chat>("POST", "/assistant/chats", { message, provider_id: providerId }),
  sendMessage: (id: string, message: string) => request<Chat>("POST", `/assistant/chats/${id}/messages`, { message }),
  decide: (id: string, callId: string, approve: boolean) =>
    request<Chat>("POST", `/assistant/chats/${id}/decisions`, { call_id: callId, approve }),
  deleteChat: (id: string) => request<void>("DELETE", `/assistant/chats/${id}`),
  automations: () => request<Automation[]>("GET", "/automations"),
  createAutomation: (input: AutomationInput) => request<Automation>("POST", "/automations", input),
  updateAutomation: (id: string, input: AutomationInput) => request<Automation>("PATCH", `/automations/${id}`, input),
  runAutomation: (id: string) => request<Chat>("POST", `/automations/${id}/run`),
  deleteAutomation: (id: string) => request<void>("DELETE", `/automations/${id}`),
  sql: (ref: string, query: string) => request<QueryResponse>("POST", `/projects/${ref}/sql`, { query }),
  tables: (ref: string) => request<TableInfo[]>("GET", `/projects/${ref}/tables`),
  createTable: (ref: string, input: { name: string; rls: boolean; columns: NewColumn[] }) =>
    request<{ sql: string }>("POST", `/projects/${ref}/tables`, input),
  rows: (ref: string, table: string, offset: number, limit: number) =>
    request<TableRows>("GET", `/projects/${ref}/tables/${encodeURIComponent(table)}/rows?offset=${offset}&limit=${limit}`),
  insertRow: (ref: string, table: string, values: Record<string, unknown>) =>
    request<void>("POST", `/projects/${ref}/tables/${encodeURIComponent(table)}/rows`, { values }),
  updateRow: (ref: string, table: string, match: Record<string, unknown>, values: Record<string, unknown>) =>
    request<void>("PATCH", `/projects/${ref}/tables/${encodeURIComponent(table)}/rows`, { match, values }),
  deleteRow: (ref: string, table: string, match: Record<string, unknown>) =>
    request<void>("DELETE", `/projects/${ref}/tables/${encodeURIComponent(table)}/rows`, { match }),
  users: (ref: string) => request<AuthUser[]>("GET", `/projects/${ref}/users`),
  createUser: (ref: string, email: string, password: string) =>
    request<unknown>("POST", `/projects/${ref}/users`, { email, password }),
  deleteUser: (ref: string, id: string) => request<unknown>("DELETE", `/projects/${ref}/users/${id}`),
  backups: (ref: string) => request<Backups>("GET", `/projects/${ref}/backups`),
  backupNow: (ref: string) => request<Backup>("POST", `/projects/${ref}/backups`),
  restore: (ref: string, target: RestoreTarget, name: string) =>
    request<Project>("POST", `/projects/${ref}/restore`, { ...target, name }),
  offsite: () => request<OffsiteList>("GET", "/offsite"),
  restoreOffsite: (ref: string, target: RestoreTarget, name: string) =>
    request<Project>("POST", `/offsite/${ref}/restore`, { ...target, name }),
  updates: () => request<Updates>("GET", "/updates"),
  checkUpdates: () => request<Updates>("POST", "/updates/check"),
  updateNow: () => request<void>("POST", "/updates"),
  logs: (ref: string, service: string) =>
    request<{ services: string[]; service: string; text: string }>("GET", `/projects/${ref}/logs?service=${service}`),
};

export const engineLabel: Record<EngineId, string> = {
  postgres: "Postgres",
  mysql: "MySQL",
  mongodb: "MongoDB-compatible",
};

export const providerLabel: Record<Region["provider"], string> = {
  docker: "Docker",
  aws: "AWS",
  gcp: "GCP",
  azure: "Azure",
};

export function formatPrice(usd: number): string {
  if (usd === 0) return "$0";
  return `$${usd.toFixed(usd % 1 === 0 ? 0 : 2)}`;
}
