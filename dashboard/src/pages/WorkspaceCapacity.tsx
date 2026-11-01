import { useEffect, useState } from "react";
import { api, formatBytes, formatMB, type AdminWorkspace, type Catalog, type WorkspaceStorage } from "../api";
import { Button, Card, Input, Notice, PageHeader } from "../ui";
import { Meter } from "./StorageUsage";

// WorkspaceCapacity is where the platform's owner gives each workspace its
// storage, the way a virtual machine is given a disk: the projects in it
// can take that much in all, counted by their plans' database sizes.
export function WorkspaceCapacity() {
  const [list, setList] = useState<AdminWorkspace[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api.adminWorkspaces().then(setList).catch((e) => setError(e.message));
  }, []);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Workspace storage"
        subtitle="Give each workspace the storage its projects may take. A project fits when its plan's database size, with the others in the workspace, stays within the limit."
      />
      {error && <Notice tone="danger">{error}</Notice>}
      {list && (
        <Card className="flex flex-col divide-y divide-border">
          {list.length === 0 && <p className="p-5 body-sm text-ink-muted">No workspaces yet.</p>}
          {list.map((w) => (
            <Row key={w.id} ws={w} onSaved={setList} />
          ))}
        </Card>
      )}
    </div>
  );
}

function Row({ ws, onSaved }: { ws: AdminWorkspace; onSaved: (l: AdminWorkspace[]) => void }) {
  const [gb, setGb] = useState(ws.limit_mb == null ? "" : String(ws.limit_mb / 1000));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);
  const current = ws.limit_mb == null ? "" : String(ws.limit_mb / 1000);

  const save = async (value: string) => {
    const n = value.trim() === "" ? null : Math.round(Number(value) * 1000);
    if (n != null && (!Number.isFinite(n) || n < 0)) {
      setError("Enter a number of GB, or leave it empty for no limit.");
      return;
    }
    setBusy(true);
    try {
      onSaved(await api.setWorkspaceStorage(ws.id, n));
      setError("");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-3 px-5 py-4">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="font-medium text-ink">{ws.name}</span>
          <span className="body-sm text-ink-muted">
            {ws.owner_email || "No owner"} · {ws.projects} {ws.projects === 1 ? "project" : "projects"} · {formatBytes(ws.used_bytes)} stored
          </span>
        </div>
        <form
          className="flex items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void save(gb);
          }}
        >
          <label className="body-sm text-ink-muted" htmlFor={`limit-${ws.id}`}>
            Limit
          </label>
          <span className="w-24 [&>input]:w-full">
            <Input id={`limit-${ws.id}`} inputMode="decimal" placeholder="No limit" value={gb} onChange={(e) => setGb(e.target.value)} />
          </span>
          <span className="body-sm text-ink-muted">GB</span>
          <Button type="submit" disabled={busy || gb.trim() === current}>
            Save
          </Button>
        </form>
      </div>
      <div className="flex flex-col gap-1">
        {ws.limit_mb != null && <Meter value={ws.allocated_mb} max={ws.limit_mb} label={`${ws.name} storage`} />}
        <span className="body-sm text-ink-muted">
          Plans take {formatMB(ws.allocated_mb)}
          {ws.limit_mb == null ? ", no limit." : ` of ${formatMB(ws.limit_mb)}.`}
          {ws.limit_mb != null && ws.allocated_mb > ws.limit_mb && " Over the limit: existing projects keep running, but new ones and bigger plans are refused."}
        </span>
      </div>
      <details
        className="group rounded-lg border border-border"
        onToggle={(e) => {
          if ((e.target as HTMLDetailsElement).open) setOpen(true);
        }}
      >
        <summary className="cursor-pointer select-none px-3 py-2 body-sm text-ink-muted hover:text-ink">Projects and plans</summary>
        {open && <Projects id={ws.id} onChanged={() => api.adminWorkspaces().then(onSaved).catch(() => {})} />}
      </details>
      {error && <Notice tone="danger">{error}</Notice>}
    </div>
  );
}

// Projects lists a workspace's projects for the platform's owner, who can
// move any of them to another plan.
function Projects({ id, onChanged }: { id: string; onChanged: () => void }) {
  const [s, setS] = useState<WorkspaceStorage | null>(null);
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  const load = () => api.adminWorkspaceProjects(id).then(setS).catch((e) => setError(e.message));
  useEffect(() => {
    void load();
    api.catalog().then(setCatalog).catch(() => {});
  }, [id]);

  const change = async (ref: string, name: string, plan: string) => {
    if (!window.confirm(`Move ${name} to ${plan}? Its database restarts with the new size.`)) return;
    setBusy(ref);
    setError("");
    try {
      await api.adminChangePlan(ref, plan);
      await load();
      onChanged();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy("");
    }
  };

  if (!s) return error ? <div className="p-3"><Notice tone="danger">{error}</Notice></div> : null;
  return (
    <div className="flex flex-col divide-y divide-border border-t border-border">
      {s.projects.length === 0 && <p className="px-3 py-2 body-sm text-ink-muted">No projects.</p>}
      {s.projects.map((p) => (
        <div key={p.ref} className="flex flex-wrap items-center justify-between gap-3 px-3 py-2">
          <div className="flex min-w-0 flex-col">
            <span className="text-[13px] font-medium text-ink">
              {p.name} <span className="font-mono text-[11px] text-ink-muted">{p.ref}</span>
            </span>
            <span className="body-sm text-ink-muted">
              {p.engine} · {p.used_bytes == null ? "not measured" : formatBytes(p.used_bytes)} of {formatMB(p.plan_mb)}
              {p.size_state === "readonly" && <span className="text-danger"> · read-only</span>}
              {(p.size_state === "warn80" || p.size_state === "warn90") && <span className="text-warning"> · over {p.size_state === "warn90" ? "90" : "80"}%</span>}
              {p.status !== "active" && ` · ${p.status}`}
            </span>
          </div>
          <select
            aria-label={`Plan for ${p.name}`}
            className="h-8 rounded-lg border border-border bg-surface-sunken px-2 text-[13px] text-ink"
            value={p.plan}
            disabled={busy === p.ref || p.status !== "active" || !catalog}
            onChange={(e) => void change(p.ref, p.name, e.target.value)}
          >
            {(catalog?.plans ?? [{ id: p.plan, name: p.plan }]).map((pl) => (
              <option key={pl.id} value={pl.id}>
                {pl.name}
              </option>
            ))}
          </select>
        </div>
      ))}
      {error && <div className="p-3"><Notice tone="danger">{error}</Notice></div>}
    </div>
  );
}
