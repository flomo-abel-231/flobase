import { useEffect, useState } from "react";
import { api, type OffsiteList } from "../api";
import { Button, Notice, PageHeader } from "../ui";
import { navigate } from "../App";
import { StorageLocations } from "./StorageLocations";

// Every project with copies in off-site storage. Projects this FloBase no
// longer runs (deleted here, or from a lost machine) can be restored as new
// projects.
export function OffsiteBackups() {
  const [list, setList] = useState<OffsiteList | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<string | null>(null);

  const load = () => {
    api
      .offsite()
      .then((l) => (setList(l), setError("")))
      .catch((e) => setError(e.message));
  };

  useEffect(load, []);

  const restore = async (ref: string, name: string) => {
    setBusy(ref);
    try {
      const p = await api.restoreOffsite(ref, { latest: true }, `${name} (restored)`);
      navigate(`/project/${p.ref}`);
    } catch (e) {
      setError((e as Error).message);
      setBusy(null);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Off-site backups"
        subtitle={
          list?.enabled ? (
            <>
              Copies in <span className="font-mono">{list.bucket}</span>
              {list.encrypted ? ", encrypted before upload" : ""}. Restoring creates a new project from the latest copy.
            </>
          ) : (
            "Copies of Postgres backups in S3-compatible storage, so a lost machine does not lose your data."
          )
        }
      />
      <StorageLocations onChange={load} />

      {error && <Notice tone="danger">{error}</Notice>}

      {list?.enabled && (
        <div className="flex flex-col gap-1">
          <h2 className="h3">Projects in storage</h2>
          <p className="body-sm text-ink-muted">Every project with copies in the active location, including ones no longer on this machine.</p>
        </div>
      )}
      {list?.enabled && list.projects.length === 0 && <p className="body-sm text-ink-muted">No copies yet; the first appear within a minute.</p>}

      {list?.enabled && list.projects.length > 0 && (
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full border-collapse text-left">
            <thead className="bg-surface-sunken">
              <tr>
                {["Project", "Full backups", "Changes up to", "Here", ""].map((h) => (
                  <th key={h} className="label border-b border-border px-3 py-2 text-ink-muted">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {list.projects.map((p) => {
                const name = p.meta.name || p.ref;
                return (
                  <tr key={p.ref} className="border-b border-border last:border-0">
                    <td className="px-3 py-2">
                      <div className="text-[14px] font-semibold text-ink">{name}</div>
                      <div className="code text-ink-muted">{p.ref}</div>
                    </td>
                    <td className="body-sm px-3 py-2 text-ink-muted">{p.backups.length}</td>
                    <td className="body-sm whitespace-nowrap px-3 py-2 text-ink-muted">
                      {p.last_change ? new Date(p.last_change).toLocaleString() : "—"}
                    </td>
                    <td className="body-sm px-3 py-2">
                      {p.here ? (
                        <a href={`#/project/${p.ref}`} className="text-brand underline-offset-2 hover:underline">
                          Running
                        </a>
                      ) : (
                        <span className="text-warning">Not on this machine</span>
                      )}
                    </td>
                    <td className="px-3 py-2 text-right">
                      <Button onClick={() => restore(p.ref, name)} disabled={busy !== null}>
                        {busy === p.ref ? "Starting…" : "Restore latest"}
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
