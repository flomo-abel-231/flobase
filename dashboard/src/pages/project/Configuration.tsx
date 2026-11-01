import { useState, type ReactNode } from "react";
import { api, engineLabel, type DeleteCheck, type Project } from "../../api";
import { Button, Card, CopyField, Field, Input, Notice } from "../../ui";
import { navigate } from "../../App";

// Configuration follows Coolify's resource settings: a vertical menu of
// sections on the left, the chosen section on the right.

const clientHint: Record<Project["engine"], string> = {
  postgres: "Use any Postgres client or driver, such as psql or node-postgres.",
  mysql: "Use any MySQL client or driver, such as mysql or mysql2.",
  mongodb: "Use any MongoDB driver or mongosh. Served by FerretDB, so a few MongoDB features are not available.",
};

export function Configuration({ project, section }: { project: Project; section?: string }) {
  const sections = [
    { id: "general", label: "General" },
    { id: "connect", label: "Connect" },
    ...(project.api_url ? [{ id: "api", label: "API" }] : []),
    { id: "danger", label: "Danger zone" },
  ];
  const current = sections.find((s) => s.id === section) ?? sections[0];

  return (
    <div className="grid gap-6 md:grid-cols-[180px_1fr]">
      <nav aria-label="Configuration" className="flex gap-1 overflow-x-auto md:flex-col">
        {sections.map((s) => (
          <a
            key={s.id}
            href={`#/project/${project.ref}/configuration/${s.id}`}
            aria-current={s.id === current.id ? "page" : undefined}
            className={`whitespace-nowrap rounded-sm px-2.5 py-1.5 text-[13px] font-medium ${
              s.id === current.id ? "bg-brand-subtle text-ink" : "text-ink-muted hover:bg-surface-sunken hover:text-ink"
            }`}
          >
            {s.label}
          </a>
        ))}
      </nav>
      <div className="min-w-0 max-w-3xl">
        {current.id === "general" && <General project={project} />}
        {current.id === "connect" && <Connect project={project} />}
        {current.id === "api" && <Api project={project} />}
        {current.id === "danger" && <Danger project={project} />}
      </div>
    </div>
  );
}

function Section({ title, subtitle, children }: { title: string; subtitle?: ReactNode; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="h3">{title}</h2>
        {subtitle && <p className="body-sm text-ink-muted">{subtitle}</p>}
      </div>
      {children}
    </section>
  );
}

function General({ project }: { project: Project }) {
  const rows: [string, ReactNode][] = [
    ["Name", project.name],
    ["Engine", engineLabel[project.engine]],
    ["Plan", project.plan],
    ["Region", project.region],
    ["Project ref", <span className="font-mono">{project.ref}</span>],
    ["Created", new Date(project.created_at).toLocaleString()],
  ];
  return (
    <Section title="General">
      <Card className="divide-y divide-border">
        {rows.map(([label, value]) => (
          <div key={label} className="flex flex-wrap items-center justify-between gap-3 px-4 py-2.5">
            <span className="label text-ink-muted">{label}</span>
            <span className="body-sm text-ink">{value}</span>
          </div>
        ))}
      </Card>
    </Section>
  );
}

function Connect({ project }: { project: Project }) {
  const c = project.connection;
  if (!c) return <p className="body-sm text-ink-muted">Connection details appear once the project is active.</p>;
  return (
    <Section title="Connect" subtitle={clientHint[project.engine]}>
      <CopyField label="Direct connection" value={c.uri} secret />
      {c.pooler_uri && (
        <>
          <CopyField label="Pooled connection (transaction mode)" value={c.pooler_uri} secret />
          <p className="body-sm text-ink-muted">
            Use the pooled connection from serverless functions and anything that opens many short connections. Prepared
            statements are not supported in transaction mode.
          </p>
        </>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        <CopyField label="Host" value={c.host} />
        <CopyField label="Port" value={String(c.port)} />
        <CopyField label="User" value={c.user} />
        <CopyField label="Database" value={c.database} />
      </div>
      <CopyField label="Password" value={c.password} secret />
    </Section>
  );
}

function Api({ project }: { project: Project }) {
  if (!project.api_url || !project.keys) return null;
  return (
    <Section title="API" subtitle="REST, GraphQL and auth for this project. Works with the Supabase client libraries unchanged.">
      <CopyField label="Project URL" value={project.api_url} />
      <CopyField label="anon key" value={project.keys.anon} />
      <CopyField label="service_role key" value={project.keys.service_role} secret />
      <p className="body-sm text-ink-muted">
        The anon key is safe in browsers once row-level security is on for your tables. Keep service_role on servers only: it
        bypasses row-level security.
      </p>
      <div className="flex flex-col gap-1.5">
        <span className="label text-ink">JavaScript</span>
        <pre className="code overflow-x-auto rounded-sm border border-border bg-surface-sunken p-3 text-ink">
          {`import { createClient } from "@supabase/supabase-js";

const db = createClient(
  "${project.api_url}",
  "<anon key>",
);

const { data } = await db.from("todos").select("*");`}
        </pre>
      </div>
    </Section>
  );
}

// Danger: deleting asks for the project's name, and when the project holds
// data, offers a backup to cloud storage first. Copies in the bucket
// outlive the project, so it can be restored from Off-site backups.
function Danger({ project }: { project: Project }) {
  const [open, setOpen] = useState(false);
  const [check, setCheck] = useState<DeleteCheck | null>(null);
  const [typed, setTyped] = useState("");
  const [skipBackup, setSkipBackup] = useState(false);
  const [backup, setBackup] = useState<{ state: "idle" | "running" | "done"; text?: string }>({ state: "idle" });
  const [error, setError] = useState("");
  const [deleting, setDeleting] = useState(false);

  const start = async () => {
    setOpen(true);
    setError("");
    try {
      setCheck(await api.deleteCheck(project.ref));
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const backUp = async () => {
    setBackup({ state: "running" });
    setError("");
    try {
      const res = await api.backupToCloud(project.ref);
      setBackup({ state: "done", text: `Backed up to ${res.bucket} at ${new Date(res.finished).toLocaleString()}.` });
    } catch (e) {
      setBackup({ state: "idle" });
      setError((e as Error).message);
    }
  };

  const remove = async () => {
    setDeleting(true);
    try {
      await api.deleteProject(project.ref, typed.trim());
      navigate("/");
    } catch (e) {
      setError((e as Error).message);
      setDeleting(false);
    }
  };

  const needsBackupChoice = !!check?.has_data && backup.state !== "done";
  const canDelete = typed.trim() === project.name && check !== null && (!needsBackupChoice || skipBackup) && !deleting;

  return (
    <Section title="Danger zone">
      <Card className="flex flex-col gap-4 border-danger p-4">
        <div className="flex flex-col gap-1">
          <h3 className="text-[14px] font-semibold text-ink">Delete project</h3>
          <p className="body-sm text-ink-muted">
            Removes the database, its services and its backups on this machine. Copies in cloud storage stay there.
          </p>
        </div>

        {!open && (
          <div>
            <Button variant="danger" onClick={start} disabled={project.status === "provisioning" || project.status === "deleting"}>
              Delete project…
            </Button>
          </div>
        )}

        {open && !check && !error && <p className="body-sm text-ink-muted">Checking what the project holds…</p>}

        {open && check && (
          <div className="flex flex-col gap-4">
            <p className="body-sm text-ink">
              <span className="font-medium">{check.has_data ? "This project holds data: " : "No data found: "}</span>
              {check.summary}
            </p>

            {check.has_data && backup.state !== "done" && (
              <div className="flex flex-col gap-3 rounded-md border border-warning bg-surface-raised p-3">
                {check.cloud_backup ? (
                  <>
                    <p className="body-sm text-ink">
                      Back it up to <span className="font-mono">{check.bucket}</span> first. You can restore it from Off-site backups
                      after it is deleted.
                    </p>
                    <div>
                      <Button variant="primary" onClick={backUp} disabled={backup.state === "running"}>
                        {backup.state === "running" ? "Backing up to cloud storage…" : `Back up to ${check.bucket}`}
                      </Button>
                    </div>
                  </>
                ) : check.engine_backup ? (
                  <p className="body-sm text-ink">
                    No cloud storage location is set up, so deleting loses this data for good.{" "}
                    <a href="#/offsite" className="text-brand underline-offset-2 hover:underline">
                      Add a storage location
                    </a>{" "}
                    to keep a copy first.
                  </p>
                ) : (
                  <p className="body-sm text-ink">
                    Backups are not available for this engine yet, so deleting loses this data for good. Export what you need first.
                  </p>
                )}
                <label className="body-sm flex items-center gap-2 text-ink">
                  <input type="checkbox" checked={skipBackup} onChange={(e) => setSkipBackup(e.target.checked)} className="size-4 accent-danger" />
                  Delete without a backup
                </label>
              </div>
            )}

            {backup.state === "done" && (
              <p className="body-sm flex items-center gap-2 text-success">
                <span className="size-2 rounded-full bg-success" aria-hidden />
                {backup.text}
              </p>
            )}

            <Field label={`Type ${project.name} to confirm`}>
              <Input value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" spellCheck={false} aria-label="Project name" />
            </Field>

            <div className="flex flex-wrap gap-2">
              <Button variant="danger" onClick={remove} disabled={!canDelete}>
                {deleting ? "Deleting…" : `Delete ${project.name}`}
              </Button>
              <Button
                onClick={() => {
                  setOpen(false);
                  setCheck(null);
                  setTyped("");
                  setSkipBackup(false);
                  setBackup({ state: "idle" });
                }}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}
        {error && <Notice tone="danger">{error}</Notice>}
      </Card>
    </Section>
  );
}
