import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, type AuthUser, type Project } from "../../api";
import { Button, Card, Field, Input, Notice } from "../../ui";

function when(iso: string | null) {
  return iso ? new Date(iso).toLocaleString() : "Never";
}

export function AuthUsers({ project }: { project: Project }) {
  const [users, setUsers] = useState<AuthUser[] | null>(null);
  const [error, setError] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setUsers(await api.users(project.ref));
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, [project.ref]);

  useEffect(() => {
    load();
  }, [load]);

  const add = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await api.createUser(project.ref, email.trim(), password);
      setEmail("");
      setPassword("");
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    try {
      await api.deleteUser(project.ref, id);
      setConfirm(null);
      await load();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <p className="body text-ink-muted">
        People who sign up to your app through <span className="font-mono">/auth/v1</span>. Email sign-up is on and addresses
        are confirmed automatically until an email server is set up.
      </p>
      {error && <Notice tone="danger">{error}</Notice>}

      <Card className="p-6">
        <form onSubmit={add} className="flex flex-col gap-4 sm:flex-row sm:items-end">
          <div className="flex-1">
            <Field label="Email">
              <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoComplete="off" />
            </Field>
          </div>
          <div className="flex-1">
            <Field label="Password">
              <Input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                minLength={6}
                autoComplete="new-password"
              />
            </Field>
          </div>
          <Button variant="primary" type="submit" disabled={busy}>
            {busy ? "Adding…" : "Add user"}
          </Button>
        </form>
      </Card>

      {users && users.length === 0 && <p className="body text-ink-muted">No users yet.</p>}
      {users && users.length > 0 && (
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full border-collapse text-left">
            <thead className="bg-surface-sunken">
              <tr>
                {["Email", "Created", "Last sign-in", "ID", ""].map((h) => (
                  <th key={h} className="label whitespace-nowrap border-b border-border px-3 py-2 text-ink-muted">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id} className="border-b border-border last:border-0">
                  <td className="body-sm px-3 py-2 text-ink">{u.email ?? "—"}</td>
                  <td className="body-sm whitespace-nowrap px-3 py-2 text-ink-muted">{when(u.created_at)}</td>
                  <td className="body-sm whitespace-nowrap px-3 py-2 text-ink-muted">{when(u.last_sign_in_at)}</td>
                  <td className="code max-w-[160px] truncate px-3 py-2 text-ink-muted" title={u.id}>
                    {u.id}
                  </td>
                  <td className="px-3 py-2 text-right">
                    {confirm === u.id ? (
                      <span className="inline-flex gap-2">
                        <Button variant="danger" onClick={() => remove(u.id)}>
                          Delete
                        </Button>
                        <Button onClick={() => setConfirm(null)}>Cancel</Button>
                      </span>
                    ) : (
                      <Button onClick={() => setConfirm(u.id)} aria-label={`Delete ${u.email ?? u.id}`}>
                        Delete
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
