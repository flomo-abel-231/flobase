import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react";
import { api, type Me, type RegionDetail, type RegionInput } from "../api";
import { Button, Card, Field, Input, Notice, PageHeader } from "../ui";

// Regions are the locations customers pick for a project. Besides this
// machine, each is a Docker server the owner adds here, such as a Hetzner
// server in Frankfurt or a Vultr one in Johannesburg. FloBase drives it over
// SSH or TLS and starts a small relay on it; new regions start closed and
// are opened once checked.

const empty: RegionInput = {
  id: "", name: "", location: "", price_multiplier: 1, transport: "ssh", endpoint: "",
  ssh_key: "", ssh_host_key: "", tls_ca: "", tls_cert: "", tls_key: "", public_host: "", relay_port: 28474,
};

const statusLabel: Record<RegionDetail["status"], { dot: string; text: string; label: string }> = {
  open: { dot: "bg-success", text: "text-success", label: "Open" },
  full: { dot: "bg-warning", text: "text-warning", label: "Full" },
  closed: { dot: "bg-ink-muted", text: "text-ink-muted", label: "Closed" },
};

export function Regions({ me }: { me: Me }) {
  const [list, setList] = useState<RegionDetail[] | null>(null);
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState("");
  const [confirm, setConfirm] = useState<string | null>(null);
  const isOwner = me.role === "owner";

  const load = useCallback(async () => {
    try {
      setList(await api.regions());
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const act = async (key: string, fn: () => Promise<unknown>, done = "") => {
    setBusy(key);
    setNote("");
    try {
      await fn();
      setConfirm(null);
      setError("");
      if (done) setNote(done);
      await load();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="flex max-w-5xl flex-col gap-6">
      <PageHeader
        title="Regions"
        subtitle="Where projects can run. Add a Docker server in each location you want to offer; its projects are managed from here like local ones."
        action={
          isOwner &&
          !adding && (
            <Button variant="primary" onClick={() => setAdding(true)}>
              + Add region server
            </Button>
          )
        }
      />
      {error && <Notice tone="danger">{error}</Notice>}
      {note && (
        <p className="body-sm flex items-center gap-2 text-success">
          <span className="size-2 rounded-full bg-success" aria-hidden />
          {note}
        </p>
      )}

      {adding && (
        <AddRegion
          onCancel={() => setAdding(false)}
          onSaved={async (r) => {
            setAdding(false);
            setNote(`${r.name} is connected. Open it when you want customers to pick it.`);
            await load();
          }}
        />
      )}

      {list && (
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full border-collapse text-left">
            <thead className="bg-surface-sunken">
              <tr>
                {["Region", "Server", "Status", "Projects", ""].map((h) => (
                  <th key={h} className="label whitespace-nowrap border-b border-border px-3 py-2 text-ink-muted">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {list.map((r) => {
                const st = statusLabel[r.status];
                return (
                  <tr key={r.id} className="border-b border-border align-top last:border-0">
                    <td className="px-3 py-2">
                      <div className="text-[14px] font-semibold text-ink">{r.name}</div>
                      <div className="body-sm text-ink-muted">
                        {r.location}
                        {r.price_multiplier !== 1 ? ` · cost × ${r.price_multiplier}` : ""}
                      </div>
                      <div className="code text-ink-muted">{r.id}</div>
                    </td>
                    <td className="px-3 py-2">
                      {r.server ? (
                        <>
                          <div className="code text-ink">{r.server.endpoint}</div>
                          <div className="body-sm text-ink-muted">
                            {r.server.transport === "ssh" ? "SSH" : "TLS"} · databases at{" "}
                            <span className="font-mono">{r.server.public_host}</span> · relay port {r.server.relay_port}
                          </div>
                          {r.server.ssh_host_key_fingerprint && (
                            <div className="code truncate text-ink-muted" title="SSH host key">
                              {r.server.ssh_host_key_fingerprint}
                            </div>
                          )}
                        </>
                      ) : (
                        <span className="body-sm text-ink-muted">The machine FloBase runs on</span>
                      )}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2">
                      <span className={`inline-flex items-center gap-2 label ${st.text}`}>
                        <span className={`size-2 rounded-full ${st.dot}`} aria-hidden />
                        {st.label}
                      </span>
                    </td>
                    <td className="metric px-3 py-2 text-ink">{r.projects}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">
                      {isOwner && (
                        <span className="inline-flex gap-2">
                          {r.status === "open" ? (
                            <Button disabled={busy !== ""} onClick={() => act(r.id, () => api.updateRegion(r.id, { status: "closed" }))}>
                              Close
                            </Button>
                          ) : (
                            <Button disabled={busy !== ""} onClick={() => act(r.id, () => api.updateRegion(r.id, { status: "open" }))}>
                              Open
                            </Button>
                          )}
                          {r.server && (
                            <Button
                              disabled={busy !== ""}
                              onClick={() => act(r.id + "-check", () => api.checkRegion(r.id), `${r.name}: Docker and the relay answer.`)}
                            >
                              {busy === r.id + "-check" ? "Checking…" : "Check"}
                            </Button>
                          )}
                          {r.server &&
                            (confirm === r.id ? (
                              <>
                                <Button variant="danger" disabled={busy !== ""} onClick={() => act(r.id, () => api.deleteRegion(r.id))}>
                                  Remove
                                </Button>
                                <Button onClick={() => setConfirm(null)}>Cancel</Button>
                              </>
                            ) : (
                              <Button disabled={r.projects > 0} onClick={() => setConfirm(r.id)} aria-label={`Remove ${r.name}`}>
                                Remove
                              </Button>
                            ))}
                        </span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <p className="body-sm text-ink-muted">
        Closing a region stops new projects there; its projects keep running. A region server can be removed once it has no
        projects. Prices in a region are its plans' estimated cost × the cost multiplier × the markup.
      </p>
    </div>
  );
}

function TextArea({ value, onChange, placeholder, rows = 4 }: { value: string; onChange: (v: string) => void; placeholder?: string; rows?: number }) {
  return (
    <textarea
      value={value}
      onChange={(e) => onChange(e.target.value)}
      rows={rows}
      spellCheck={false}
      autoComplete="off"
      placeholder={placeholder}
      className="code rounded-sm border border-border-strong bg-surface-sunken px-2.5 py-1.5 text-ink placeholder:text-ink-muted"
    />
  );
}

function Choice({ checked, onChange, children }: { checked: boolean; onChange: () => void; children: ReactNode }) {
  return (
    <label
      className={`flex cursor-pointer flex-col gap-0.5 rounded-md border p-3 has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-brand ${
        checked ? "border-brand bg-brand-subtle" : "border-border-strong bg-surface-raised hover:bg-surface-sunken"
      }`}
    >
      <input type="radio" name="transport" checked={checked} onChange={onChange} className="sr-only" />
      {children}
    </label>
  );
}

function AddRegion({ onCancel, onSaved }: { onCancel: () => void; onSaved: (r: RegionDetail) => void }) {
  const [f, setF] = useState<RegionInput>(empty);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = (patch: Partial<RegionInput>) => {
    setF((cur) => ({ ...cur, ...patch }));
    setError("");
  };

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      onSaved(await api.addRegion(f));
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  return (
    <Card className="p-5">
      <form onSubmit={save} className="flex flex-col gap-5">
        <h2 className="text-[14px] font-semibold text-ink">Add a region server</h2>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name" hint="What customers see, such as Frankfurt.">
            <Input value={f.name} onChange={(e) => set({ name: e.target.value })} required maxLength={64} autoFocus placeholder="Frankfurt" />
          </Field>
          <Field label="Location label" hint="Shown under the name.">
            <Input value={f.location} onChange={(e) => set({ location: e.target.value })} maxLength={64} placeholder="Germany · Hetzner" />
          </Field>
          <Field label="Region id" hint="Permanent; lowercase letters, digits and dashes.">
            <Input value={f.id} onChange={(e) => set({ id: e.target.value.toLowerCase() })} required pattern="[a-z][a-z0-9-]{1,31}" placeholder="eu-fra-1" />
          </Field>
          <Field label="Cost multiplier" hint="The server's cost relative to your cheapest one; prices scale with it.">
            <Input
              type="number"
              min={0.1}
              max={10}
              step={0.05}
              value={f.price_multiplier}
              onChange={(e) => set({ price_multiplier: Number(e.target.value) })}
            />
          </Field>
        </div>

        <fieldset className="flex flex-col gap-3">
          <legend className="label mb-1.5 text-ink">How FloBase reaches Docker on the server</legend>
          <div className="grid gap-3 sm:grid-cols-2">
            <Choice checked={f.transport === "ssh"} onChange={() => set({ transport: "ssh" })}>
              <span className="font-medium text-ink">SSH</span>
              <span className="body-sm text-ink-muted">Sign in with a key, like docker -H ssh://. Nothing else to set up.</span>
            </Choice>
            <Choice checked={f.transport === "tls"} onChange={() => set({ transport: "tls" })}>
              <span className="font-medium text-ink">TLS with client certificates</span>
              <span className="body-sm text-ink-muted">dockerd --tlsverify on port 2376.</span>
            </Choice>
          </div>
          {f.transport === "ssh" ? (
            <div className="grid gap-4">
              <Field label="SSH endpoint" hint="A user in the server's docker group, or root.">
                <Input value={f.endpoint} onChange={(e) => set({ endpoint: e.target.value })} required placeholder="ssh://root@203.0.113.10" />
              </Field>
              <Field label="Private key">
                <TextArea value={f.ssh_key} onChange={(v) => set({ ssh_key: v })} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" />
              </Field>
              <Field label="Host key (recommended)" hint="From ssh-keyscan -t ed25519 <host>. Left empty, the key seen on first connect is trusted from then on.">
                <TextArea rows={2} value={f.ssh_host_key} onChange={(v) => set({ ssh_host_key: v })} placeholder="ssh-ed25519 AAAAC3Nza…" />
              </Field>
            </div>
          ) : (
            <div className="grid gap-4">
              <Field label="TLS endpoint">
                <Input value={f.endpoint} onChange={(e) => set({ endpoint: e.target.value })} required placeholder="tcp://203.0.113.10:2376" />
              </Field>
              <div className="grid gap-4 lg:grid-cols-3">
                <Field label="CA certificate (ca.pem)">
                  <TextArea value={f.tls_ca} onChange={(v) => set({ tls_ca: v })} placeholder="-----BEGIN CERTIFICATE-----" />
                </Field>
                <Field label="Client certificate (cert.pem)">
                  <TextArea value={f.tls_cert} onChange={(v) => set({ tls_cert: v })} placeholder="-----BEGIN CERTIFICATE-----" />
                </Field>
                <Field label="Client key (key.pem)">
                  <TextArea value={f.tls_key} onChange={(v) => set({ tls_key: v })} placeholder="-----BEGIN PRIVATE KEY-----" />
                </Field>
              </div>
            </div>
          )}
        </fieldset>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Public host" hint="The name customers' database connection strings use; point its DNS at the server.">
            <Input value={f.public_host} onChange={(e) => set({ public_host: e.target.value })} required placeholder="fra1.db.example.com" />
          </Field>
          <Field label="Relay port" hint="FloBase's gateway reaches REST and auth through it. Allow it from this FloBase's address only.">
            <Input type="number" min={1024} max={65535} value={f.relay_port} onChange={(e) => set({ relay_port: Number(e.target.value) })} />
          </Field>
        </div>

        {error && <Notice tone="danger">{error}</Notice>}
        <div className="flex flex-wrap gap-2">
          <Button variant="primary" type="submit" disabled={busy}>
            {busy ? "Connecting and starting the relay…" : "Connect and add"}
          </Button>
          <Button type="button" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
        </div>
      </form>
    </Card>
  );
}
