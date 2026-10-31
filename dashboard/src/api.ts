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
  can_update: boolean;
  updating: boolean;
  checked_at?: string;
  error?: string;
  manual_steps?: string;
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
}

let memoryToken = getToken();

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public detail?: { hint?: string; detail?: string; position?: number },
  ) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${memoryToken}`,
      ...(body ? { "Content-Type": "application/json" } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
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
  if (res.status === 204 || (res.status === 202 && method === "DELETE")) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

export const api = {
  config: () => request<ServerConfig>("GET", "/config"),
  me: () => request<{ role: string }>("GET", "/me"),
  catalog: () => request<Catalog>("GET", "/catalog"),
  projects: () => request<Project[]>("GET", "/projects"),
  project: (ref: string) => request<Project>("GET", `/projects/${ref}`),
  createProject: (input: { name: string; engine: EngineId; region: string; plan: string }) =>
    request<Project>("POST", "/projects", input),
  deleteProject: (ref: string) => request<void>("DELETE", `/projects/${ref}`),
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
