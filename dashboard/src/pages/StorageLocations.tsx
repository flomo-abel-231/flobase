import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, type StorageInput, type StorageLocation } from "../api";
import { Button, Card, Field, Input, Notice } from "../ui";

// Storage locations for off-site backup copies, in the style of Coolify's
// S3 storages: a list with the active one marked, and a form to add more.
// The form tests the bucket before saving; keys never come back from the
// server, only their last characters.

type Provider = { id: string; label: string; region: string; endpoint: (region: string) => string; hint: string };

const providers: Provider[] = [
  { id: "aws", label: "AWS S3", region: "us-east-1", endpoint: (r) => `https://s3.${r}.amazonaws.com`, hint: "IAM user with s3:PutObject, GetObject, DeleteObject and ListBucket on the bucket." },
  { id: "r2", label: "Cloudflare R2", region: "auto", endpoint: () => "https://<account-id>.r2.cloudflarestorage.com", hint: "Replace <account-id> with yours; create an R2 API token with Object Read & Write." },
  { id: "b2", label: "Backblaze B2", region: "us-west-004", endpoint: (r) => `https://s3.${r}.backblazeb2.com`, hint: "Use an application key's keyID and key; the region is in the bucket's endpoint." },
  { id: "wasabi", label: "Wasabi", region: "us-east-1", endpoint: (r) => `https://s3.${r}.wasabisys.com`, hint: "Use an access key with access to the bucket." },
  { id: "custom", label: "MinIO or other S3-compatible", region: "us-east-1", endpoint: () => "", hint: "Any service that speaks the S3 API." },
];

const empty: StorageInput = {
  name: "", provider: "aws", endpoint: providers[0].endpoint(providers[0].region), region: providers[0].region,
  bucket: "", prefix: "flobase", access_key: "", secret_key: "", encryption_key: "",
};

function newKey(): string {
  const b = new Uint8Array(32);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

export function StorageLocations({ onChange }: { onChange: () => void }) {
  const [list, setList] = useState<StorageLocation[] | null>(null);
  const [envManaged, setEnvManaged] = useState(false);
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await api.storages();
      setList(res.locations);
      setEnvManaged(res.env_managed);
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
      setConfirm(null);
      await load();
      onChange();
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="h3">Storage locations</h2>
        {!adding && (
          <Button variant="primary" onClick={() => setAdding(true)}>
            + Add
          </Button>
        )}
      </div>
      <p className="body-sm text-ink-muted">
        S3-compatible buckets for backup copies. Every Postgres project's backups are copied to the active location within 30 seconds.
      </p>
      {envManaged && (
        <Notice tone="warning">
          Off-site storage is set in <span className="font-mono">.env</span>, so that location is used. Remove the
          FLOBASE_BACKUP_S3_ settings there to choose one here.
        </Notice>
      )}
      {error && <Notice tone="danger">{error}</Notice>}

      {adding && (
        <AddLocation
          onCancel={() => setAdding(false)}
          onSaved={async () => {
            setAdding(false);
            await load();
            onChange();
          }}
        />
      )}

      {list && list.length === 0 && !adding && (
        <Card className="flex flex-col gap-1 p-4">
          <span className="text-[14px] font-semibold text-ink">No storage location yet</span>
          <span className="body-sm text-ink-muted">Backups are only on this machine. Add a bucket to keep copies elsewhere.</span>
        </Card>
      )}

      {list && list.length > 0 && (
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full border-collapse text-left">
            <thead className="bg-surface-sunken">
              <tr>
                {["Name", "Bucket", "Endpoint", "Keys", ""].map((h) => (
                  <th key={h} className="label whitespace-nowrap border-b border-border px-3 py-2 text-ink-muted">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {list.map((l) => (
                <tr key={l.id} className="border-b border-border last:border-0">
                  <td className="px-3 py-2">
                    <div className="flex items-center gap-2">
                      <span className="text-[14px] font-semibold text-ink">{l.name}</span>
                      {l.active && (
                        <span className="inline-flex items-center gap-1.5 label text-success">
                          <span className="size-2 rounded-full bg-success" aria-hidden />
                          Active
                        </span>
                      )}
                    </div>
                    <div className="body-sm text-ink-muted">{providers.find((p) => p.id === l.provider)?.label ?? l.provider}</div>
                  </td>
                  <td className="code px-3 py-2 text-ink">
                    {l.bucket}
                    {l.prefix ? `/${l.prefix}` : ""}
                  </td>
                  <td className="code max-w-[240px] truncate px-3 py-2 text-ink-muted" title={l.endpoint}>
                    {l.endpoint}
                  </td>
                  <td className="body-sm whitespace-nowrap px-3 py-2 text-ink-muted">
                    <span className="font-mono">{l.access_key_hint}</span>
                    {l.encrypted ? " · encrypted" : ""}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-right">
                    {!l.from_env && (
                      <span className="inline-flex gap-2">
                        {l.active ? (
                          <Button onClick={() => act(() => api.deactivateStorage())}>Stop copying</Button>
                        ) : (
                          !envManaged && <Button onClick={() => act(() => api.activateStorage(l.id))}>Make active</Button>
                        )}
                        {confirm === l.id ? (
                          <>
                            <Button variant="danger" onClick={() => act(() => api.deleteStorage(l.id))}>
                              Remove
                            </Button>
                            <Button onClick={() => setConfirm(null)}>Cancel</Button>
                          </>
                        ) : (
                          <Button onClick={() => setConfirm(l.id)} aria-label={`Remove ${l.name}`}>
                            Remove
                          </Button>
                        )}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {confirm && (
        <p className="body-sm text-ink-muted">
          Removing a location stops new copies to it. Copies already in the bucket stay there.
        </p>
      )}
    </section>
  );
}

function AddLocation({ onCancel, onSaved }: { onCancel: () => void; onSaved: () => void }) {
  const [f, setF] = useState<StorageInput>(empty);
  const [status, setStatus] = useState<{ tone: "danger" | "ok"; text: string } | null>(null);
  const [busy, setBusy] = useState<"" | "test" | "save">("");
  const provider = providers.find((p) => p.id === f.provider) ?? providers[0];

  const set = (patch: Partial<StorageInput>) => {
    setF((cur) => ({ ...cur, ...patch }));
    setStatus(null);
  };

  const pickProvider = (id: string) => {
    const p = providers.find((x) => x.id === id) ?? providers[0];
    set({ provider: p.id, region: p.region, endpoint: p.endpoint(p.region) });
  };

  const pickRegion = (region: string) => {
    if (["aws", "b2", "wasabi"].includes(f.provider)) set({ region, endpoint: provider.endpoint(region) });
    else set({ region });
  };

  const test = async () => {
    setBusy("test");
    try {
      await api.testStorage(f);
      setStatus({ tone: "ok", text: "Connected: FloBase wrote, read and deleted a test file in the bucket." });
    } catch (e) {
      setStatus({ tone: "danger", text: (e as Error).message });
    } finally {
      setBusy("");
    }
  };

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy("save");
    try {
      await api.createStorage(f);
      onSaved();
    } catch (err) {
      setStatus({ tone: "danger", text: (err as Error).message });
      setBusy("");
    }
  };

  return (
    <Card className="p-5">
      <form onSubmit={save} className="flex flex-col gap-4">
        <h3 className="text-[14px] font-semibold text-ink">Add a storage location</h3>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name">
            <Input value={f.name} onChange={(e) => set({ name: e.target.value })} placeholder="Backups on R2" required maxLength={64} autoFocus />
          </Field>
          <Field label="Provider">
            <select
              value={f.provider}
              onChange={(e) => pickProvider(e.target.value)}
              className="h-8 rounded-sm border border-border-strong bg-surface-sunken px-2 text-[14px] text-ink"
            >
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.label}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Endpoint" hint={provider.hint}>
            <Input value={f.endpoint} onChange={(e) => set({ endpoint: e.target.value })} placeholder="https://s3.example.com" required />
          </Field>
          <Field label="Region">
            <Input value={f.region} onChange={(e) => pickRegion(e.target.value)} placeholder="us-east-1" />
          </Field>
          <Field label="Bucket">
            <Input value={f.bucket} onChange={(e) => set({ bucket: e.target.value })} placeholder="my-flobase-backups" required />
          </Field>
          <Field label="Folder in the bucket" hint="Optional. Lets several FloBases share one bucket.">
            <Input value={f.prefix} onChange={(e) => set({ prefix: e.target.value })} placeholder="flobase" />
          </Field>
          <Field label="Access key">
            <Input value={f.access_key} onChange={(e) => set({ access_key: e.target.value })} required autoComplete="off" spellCheck={false} />
          </Field>
          <Field label="Secret key">
            <Input type="password" value={f.secret_key} onChange={(e) => set({ secret_key: e.target.value })} required autoComplete="new-password" />
          </Field>
        </div>
        <Field
          label="Encryption key (recommended)"
          hint="Encrypts backups and file names on this machine before upload, so the storage provider cannot read them. Save the key somewhere other than this server: without it the copies cannot be restored."
        >
          <div className="flex gap-2">
            <div className="flex min-w-0 flex-1 flex-col">
              <Input value={f.encryption_key} onChange={(e) => set({ encryption_key: e.target.value })} placeholder="Leave empty to upload unencrypted" spellCheck={false} />
            </div>
            <Button type="button" onClick={() => set({ encryption_key: newKey() })}>
              Generate
            </Button>
          </div>
        </Field>

        {status && (
          status.tone === "ok" ? (
            <p className="body-sm flex items-center gap-2 text-success">
              <span className="size-2 rounded-full bg-success" aria-hidden />
              {status.text}
            </p>
          ) : (
            <Notice tone="danger">{status.text}</Notice>
          )
        )}

        <div className="flex flex-wrap gap-2">
          <Button variant="primary" type="submit" disabled={busy !== ""}>
            {busy === "save" ? "Testing and saving…" : "Save"}
          </Button>
          <Button type="button" onClick={test} disabled={busy !== ""}>
            {busy === "test" ? "Testing…" : "Test connection"}
          </Button>
          <Button type="button" onClick={onCancel} disabled={busy !== ""}>
            Cancel
          </Button>
        </div>
      </form>
    </Card>
  );
}
