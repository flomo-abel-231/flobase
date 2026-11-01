import { useEffect, useState } from "react";
import { api, daysUntil, formatBytes, formatMB, type WorkspaceStorage } from "../api";
import { Card, Notice } from "../ui";

// Meter is a bar filled to a share of its whole.
export function Meter({ value, max, label }: { value: number; max: number; label: string }) {
  const share = max > 0 ? Math.min(value / max, 1) : 0;
  const tone = share >= 0.9 ? "bg-danger" : share >= 0.75 ? "bg-warning" : "bg-brand";
  return (
    <div role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={max} aria-valuenow={value} className="h-2 w-full overflow-hidden rounded-full bg-surface-sunken">
      <div className={`h-full rounded-full ${tone}`} style={{ width: `${Math.max(share * 100, share > 0 ? 2 : 0)}%` }} />
    </div>
  );
}

// StorageUsage shows a workspace's storage: what its projects' plans take
// against the limit the platform's owner set, and what each database holds.
export function StorageUsage() {
  const [s, setS] = useState<WorkspaceStorage | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api.storage().then(setS).catch((e) => setError(e.message));
  }, []);

  if (error) return <Notice tone="danger">{error}</Notice>;
  if (!s) return null;

  return (
    <Card className="flex flex-col gap-5 p-5">
      <div className="flex flex-col gap-1">
        <h2 className="text-[14px] font-semibold text-ink">Storage</h2>
        <p className="body-sm text-ink-muted">
          Each plan comes with a database size. {s.limit_mb == null ? "This workspace has no storage limit." : "Together they must fit in this workspace's storage."}
        </p>
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        <Stat label="Capacity" value={s.limit_mb == null ? "No limit" : formatMB(s.limit_mb)} />
        <Stat label="Taken by plans" value={formatMB(s.allocated_mb)} />
        <Stat label="Data stored" value={formatBytes(s.used_bytes)} />
      </div>
      {s.limit_mb != null && (
        <div className="flex flex-col gap-1.5">
          <Meter value={s.allocated_mb} max={s.limit_mb} label="Storage taken by plans" />
          <span className="body-sm text-ink-muted">
            {formatMB(Math.max(s.limit_mb - s.allocated_mb, 0))} left for new projects or bigger plans.
          </span>
        </div>
      )}

      {s.projects.length > 0 && (
        <div className="flex flex-col divide-y divide-border rounded-lg border border-border">
          {s.projects.map((p) => {
            const used = p.used_bytes ?? 0;
            return (
              <a key={p.ref} href={`#/project/${p.ref}/configuration`} className="flex flex-col gap-2 px-4 py-3 hover:bg-surface-sunken">
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                  <span className="font-medium text-ink">
                    {p.name} <span className="body-sm font-normal text-ink-muted capitalize">· {p.plan}</span>
                  </span>
                  <span className="body-sm text-ink-muted">
                    {p.used_bytes == null ? "Not measured yet" : formatBytes(p.used_bytes)} of {formatMB(p.plan_mb)}
                  </span>
                </div>
                <Meter value={used / 1e6} max={p.plan_mb} label={`${p.name} storage`} />
                {p.size_state === "readonly" && <span className="body-sm text-danger">Read-only: over its plan's size. Delete data or move to a bigger plan.</span>}
                {(p.size_state === "warn80" || p.size_state === "warn90") && (
                  <span className="body-sm text-warning">Over {p.size_state === "warn90" ? "90" : "80"}% of its plan's size.</span>
                )}
                {p.expires_at && (
                  <span className="body-sm text-warning">Deleted in {Math.max(daysUntil(p.expires_at), 0)} days (Free plan)</span>
                )}
              </a>
            );
          })}
        </div>
      )}
      <p className="body-sm text-ink-muted">
        Database sizes are measured every 10 minutes. A database that grows past its plan's size turns read-only until it fits again.
      </p>
    </Card>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-lg bg-surface-sunken px-3 py-2.5">
      <span className="body-sm text-ink-muted">{label}</span>
      <span className="text-[18px] font-semibold text-ink">{value}</span>
    </div>
  );
}
