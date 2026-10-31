import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, type Backups as BackupsInfo, type Project, type RestoreTarget } from "../../api";
import { Button, Card, Field, Input, Notice } from "../../ui";
import { navigate } from "../../App";

type Mode = "latest" | "time" | "backup";

function when(iso: string) {
  return new Date(iso).toLocaleString();
}

function size(bytes: number) {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`;
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`;
  return `${Math.max(1, Math.round(bytes / 1024))} KB`;
}

// <input type="datetime-local"> works in local time without a zone.
function toLocalInput(iso: string) {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function Backups({ project }: { project: Project }) {
  const [info, setInfo] = useState<BackupsInfo | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [mode, setMode] = useState<Mode>("latest");
  const [time, setTime] = useState("");
  const [backup, setBackup] = useState("");
  const [name, setName] = useState(`${project.name} (restored)`);
  const [restoring, setRestoring] = useState(false);

  const load = useCallback(async () => {
    try {
      const b = await api.backups(project.ref);
      setInfo(b);
      setError("");
      if (!b.point_in_time) setMode("backup");
      setBackup((cur) => cur || b.backups.at(-1)?.name || "");
    } catch (e) {
      setError((e as Error).message);
    }
  }, [project.ref]);

  useEffect(() => {
    load();
  }, [load]);

  const backupNow = async () => {
    setBusy(true);
    try {
      await api.backupNow(project.ref);
      await load();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const restore = async (e: FormEvent) => {
    e.preventDefault();
    let target: RestoreTarget;
    if (mode === "latest") target = { latest: true };
    else if (mode === "time") target = { time: new Date(time).toISOString() };
    else target = { backup };
    setRestoring(true);
    try {
      const p = await api.restore(project.ref, target, name.trim());
      navigate(`/project/${p.ref}`);
    } catch (err) {
      setError((err as Error).message);
      setRestoring(false);
    }
  };

  if (!info) return error ? <Notice tone="warning">{error}</Notice> : null;
  if (!info.enabled) {
    return <Notice tone="warning">Backups start on the Starter plan. Upgrade the project to turn them on.</Notice>;
  }

  const newest = info.backups.at(-1);
  const canRestore = info.backups.length > 0 && (mode !== "time" || time !== "");

  return (
    <div className="flex max-w-4xl flex-col gap-6">
      {error && <Notice tone="danger">{error}</Notice>}

      <div className="grid gap-4 sm:grid-cols-3">
        <Card className="flex flex-col gap-1 p-4">
          <span className="label text-ink-muted">Recovery</span>
          <span className="h3">{info.point_in_time ? "Point in time" : "Daily backups"}</span>
        </Card>
        <Card className="flex flex-col gap-1 p-4">
          <span className="label text-ink-muted">Kept for</span>
          <span className="h3">
            {info.retention_days} {info.retention_days === 1 ? "day" : "days"}
          </span>
        </Card>
        <Card className="flex flex-col gap-1 p-4">
          <span className="label text-ink-muted">Last full backup</span>
          <span className="h3">{newest ? when(newest.finished) : "Not yet"}</span>
        </Card>
      </div>

      <OffsiteLine offsite={info.offsite} />

      <Card className="p-6">
        <form onSubmit={restore} className="flex flex-col gap-5">
          <div className="flex flex-col gap-1">
            <h2 className="h3">Restore to a new project</h2>
            <p className="body-sm text-ink-muted">
              {info.window
                ? `Any moment from ${when(info.window.from)} until now. Every change is archived within a minute.`
                : "Pick one of the daily backups below."}{" "}
              This project is not changed. The new project gets its own keys and password, and its users sign in again.
            </p>
          </div>

          <fieldset className="flex flex-col gap-3">
            <legend className="sr-only">Restore point</legend>
            {info.point_in_time && (
              <>
                <label className="flex items-center gap-3">
                  <input type="radio" name="mode" checked={mode === "latest"} onChange={() => setMode("latest")} className="size-4 accent-brand" />
                  <span className="body text-ink">Latest: everything up to now</span>
                </label>
                <label className="flex flex-wrap items-center gap-3">
                  <input type="radio" name="mode" checked={mode === "time"} onChange={() => setMode("time")} className="size-4 accent-brand" />
                  <span className="body text-ink">A point in time</span>
                  {mode === "time" && info.window && (
                    <input
                      type="datetime-local"
                      step={1}
                      aria-label="Restore time"
                      value={time}
                      min={toLocalInput(info.window.from)}
                      max={toLocalInput(info.window.to)}
                      onChange={(e) => setTime(e.target.value)}
                      required
                      className="code h-9 rounded-md border border-border-strong bg-surface-raised px-2 text-ink"
                    />
                  )}
                </label>
              </>
            )}
            <label className="flex flex-wrap items-center gap-3">
              <input type="radio" name="mode" checked={mode === "backup"} onChange={() => setMode("backup")} className="size-4 accent-brand" />
              <span className="body text-ink">A full backup</span>
              {mode === "backup" && (
                <select
                  aria-label="Backup"
                  value={backup}
                  onChange={(e) => setBackup(e.target.value)}
                  className="h-9 rounded-md border border-border-strong bg-surface-raised px-2 text-[15px] text-ink"
                >
                  {[...info.backups].reverse().map((b) => (
                    <option key={b.name} value={b.name}>
                      {when(b.finished)}
                    </option>
                  ))}
                </select>
              )}
            </label>
          </fieldset>

          <Field label="New project name">
            <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={64} required />
          </Field>

          <div>
            <Button variant="primary" type="submit" disabled={restoring || !canRestore || !name.trim()}>
              {restoring ? "Starting restore…" : "Restore to a new project"}
            </Button>
          </div>
        </form>
      </Card>

      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="h3">Full backups</h2>
          <Button onClick={backupNow} disabled={busy || info.running}>
            {busy || info.running ? "Backing up…" : "Back up now"}
          </Button>
        </div>
        <p className="body-sm text-ink-muted">
          Taken daily and when the project is created. Restores replay archived changes on top of the nearest one.
        </p>
        {info.backups.length === 0 ? (
          <p className="body text-ink-muted">No full backups yet.</p>
        ) : (
          <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
            <table className="w-full border-collapse text-left">
              <thead className="bg-surface-sunken">
                <tr>
                  {["Finished", "Size", "Name"].map((h) => (
                    <th key={h} className="label border-b border-border px-3 py-2 text-ink-muted">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {[...info.backups].reverse().map((b) => (
                  <tr key={b.name} className="border-b border-border last:border-0">
                    <td className="body-sm whitespace-nowrap px-3 py-2 text-ink">{when(b.finished)}</td>
                    <td className="body-sm whitespace-nowrap px-3 py-2 text-ink-muted">{size(b.size_bytes)}</td>
                    <td className="code px-3 py-2 text-ink-muted">{b.name}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

function OffsiteLine({ offsite }: { offsite: BackupsInfo["offsite"] }) {
  if (!offsite.enabled) {
    return (
      <Notice tone="warning">
        Backups are only on this machine, so losing it loses them too. Set FLOBASE_BACKUP_S3_BUCKET and its keys in .env to copy
        them to S3-compatible storage.
      </Notice>
    );
  }
  if (offsite.failing) {
    return (
      <Notice tone="danger">
        Copying to {offsite.bucket} is failing. Check the bucket and keys, then the off-site sync logs on the Logs tab.
      </Notice>
    );
  }
  return (
    <p className="body-sm flex items-center gap-2 text-ink-muted">
      <span className="size-2 rounded-full bg-success" aria-hidden />
      <span>
        <span className="font-medium text-success">Off-site copy on.</span>{" "}
        {offsite.encrypted ? "Encrypted on this machine and copied" : "Copied"} to <span className="font-mono">{offsite.bucket}</span>
        {offsite.last_sync ? `, last ${when(offsite.last_sync)}.` : ", first copy in progress."}
      </span>
    </p>
  );
}
