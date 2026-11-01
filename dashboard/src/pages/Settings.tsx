import { useEffect, useState, type ReactNode } from "react";
import { api, type ServerConfig, type Updates } from "../api";
import { Button, Card, Notice, PageHeader, ThemeSwitch } from "../ui";

export function Settings({ config }: { config: ServerConfig | null }) {
  return (
    <div className="flex max-w-3xl flex-col gap-8">
      <PageHeader title="Settings" subtitle="This FloBase instance and how the dashboard looks to you." />

      <Card className="flex flex-col divide-y divide-border">
        <Row label="Mode" value={config ? (config.mode === "cloud" ? "Cloud: plans with prices, regions on your servers" : "Self-hosted: one organisation, no billing") : "—"} />
        <Row label="Theme" value={<ThemeSwitch />} />
      </Card>

      <UpdatesSection />

      <p className="body-sm text-ink-muted">
        Instance settings such as off-site storage, the gateway URL, auto-update and the API token are set in{" "}
        <span className="font-mono">.env</span> and take effect when FloBase restarts.
      </p>
    </div>
  );
}

function UpdatesSection() {
  const [u, setU] = useState<Updates | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [started, setStarted] = useState(false);

  useEffect(() => {
    api.updates().then(setU).catch((e) => setError(e.message));
  }, []);

  const check = async () => {
    setBusy(true);
    try {
      setU(await api.checkUpdates());
      setError("");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const update = async () => {
    setBusy(true);
    try {
      await api.updateNow();
      setStarted(true);
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
    }
  };

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="h3">Updates</h2>
        <Button onClick={check} disabled={busy}>
          Check now
        </Button>
      </div>
      {error && <Notice tone="danger">{error}</Notice>}
      {started || u?.updating ? (
        <Notice tone="warning">
          Updating. FloBase backs up its own database, pulls the new version and restarts; the dashboard comes back in a minute or two.
          Your project databases keep running throughout.
        </Notice>
      ) : null}
      {u && (
        <Card className="flex flex-col divide-y divide-border">
          <Row label="Running" value={<span className="font-mono">{u.current}</span>} />
          <Row
            label="Latest release"
            value={
              u.latest ? (
                u.release_url ? (
                  <a href={u.release_url} target="_blank" rel="noreferrer" className="font-mono text-brand underline-offset-2 hover:underline">
                    {u.latest}
                  </a>
                ) : (
                  <span className="font-mono">{u.latest}</span>
                )
              ) : (
                "Not checked yet"
              )
            }
          />
          <Row
            label="Status"
            value={
              u.available ? (
                <span className="font-medium text-warning">Update available</span>
              ) : u.latest ? (
                <span className="text-success">Up to date</span>
              ) : (
                "—"
              )
            }
          />
          <Row
            label="Automatic updates"
            value={
              !u.can_update
                ? u.install === "local"
                  ? "Off: update from FloBase Desktop or with flobase update"
                  : "Off: not installed with the install script"
                : u.auto
                  ? "On: installs new releases within 6 hours"
                  : "Off: set FLOBASE_AUTO_UPDATE=true to turn on"
            }
          />
        </Card>
      )}
      {u?.available && !started && !u.updating && (
        <div className="flex flex-col gap-2">
          {u.can_update ? (
            <div>
              <Button variant="primary" onClick={update} disabled={busy}>
                Update to {u.latest}
              </Button>
            </div>
          ) : (
            <p className="body-sm text-ink-muted">
              {u.install === "local" ? "Update with Check for updates in FloBase Desktop, or run " : "Update by hand on the server: "}
              <span className="font-mono">{u.manual_steps}</span>
            </p>
          )}
          {u.notes && (
            <details className="rounded-md border border-border bg-surface-raised p-3">
              <summary className="body-sm cursor-pointer text-ink">What's new in {u.latest}</summary>
              <pre className="body-sm mt-2 whitespace-pre-wrap font-sans text-ink-muted">{u.notes}</pre>
            </details>
          )}
        </div>
      )}
    </section>
  );
}

function Row({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-2.5">
      <span className="label text-ink-muted">{label}</span>
      <span className="body-sm text-ink">{value}</span>
    </div>
  );
}
