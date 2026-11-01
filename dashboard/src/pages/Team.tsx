import { useEffect, useState, type FormEvent } from "react";
import { api, type Invite, type Me, type User } from "../api";
import { Button, Card, CopyField, Field, Input, Notice, PageHeader, Tag } from "../ui";

const roleLabel: Record<User["role"], string> = { owner: "Owner", admin: "Admin", member: "Member" };

// Team lists who can sign in. The owner and admins invite people with a
// link they send themselves, change roles and remove people; members see
// the list only.
export function Team({ me }: { me: Me }) {
  const [users, setUsers] = useState<User[] | null>(null);
  const [invites, setInvites] = useState<Invite[]>([]);
  const [error, setError] = useState("");
  const [link, setLink] = useState<{ email: string; url: string } | null>(null);
  // Roles here are roles in the current workspace.
  const role = me.workspaces.find((w) => w.id === me.workspace)?.role;
  const canManage = role === "owner" || role === "admin";

  const load = () =>
    api
      .team()
      .then((t) => {
        setUsers(t.users);
        setInvites(t.invites);
      })
      .catch((e) => setError(e.message));

  useEffect(() => {
    void load();
  }, []);

  const act = async (fn: () => Promise<void>) => {
    setError("");
    try {
      await fn();
      await load();
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <div className="flex max-w-4xl flex-col gap-8">
      <PageHeader
        title="Members"
        subtitle={`People in ${me.workspaces.find((w) => w.id === me.workspace)?.name ?? "this workspace"}. Members use its projects; its owner and admins also invite and manage people. Other workspaces cannot see them or their projects.`}
      />
      {error && <Notice tone="danger">{error}</Notice>}

      {canManage && (
        <InviteForm
          onInvited={(email, token) => {
            setLink({ email, url: `${window.location.origin}${window.location.pathname}#/invite/${token}` });
            void load();
          }}
        />
      )}
      {link && (
        <Card className="flex flex-col gap-3 border-brand p-4">
          <CopyField label={`Invite link for ${link.email}`} value={link.url} />
          <p className="body-sm text-ink-muted">
            Send this link yourself; FloBase does not send email yet. It works once and expires in 7 days. Anyone with the link can
            join as this person, so send it privately.
          </p>
        </Card>
      )}

      <section className="flex flex-col gap-3">
        <h2 className="h3">People</h2>
        <Card className="divide-y divide-border">
          {!users && <p className="body-sm px-4 py-3 text-ink-muted">Loading…</p>}
          {users?.map((u) => {
            const self = me.user?.id === u.id;
            return (
              <div key={u.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
                <div className="flex min-w-0 flex-col">
                  <span className="truncate text-[14px] font-medium text-ink">
                    {u.name || u.email}
                    {self && <span className="text-ink-muted"> (you)</span>}
                  </span>
                  {u.name && <span className="body-sm truncate text-ink-muted">{u.email}</span>}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  {canManage && u.role !== "owner" && !self ? (
                    <>
                      <select
                        aria-label={`Role of ${u.email}`}
                        value={u.role}
                        onChange={(e) => act(() => api.setRole(u.id, e.target.value as Invite["role"]))}
                        className="h-8 rounded-sm border border-border-strong bg-surface-sunken px-2 text-[13px] text-ink"
                      >
                        <option value="admin">Admin</option>
                        <option value="member">Member</option>
                      </select>
                      <Button
                        variant="danger"
                        onClick={() => {
                          if (window.confirm(`Remove ${u.email}? They are signed out at once.`)) void act(() => api.removeUser(u.id));
                        }}
                      >
                        Remove
                      </Button>
                    </>
                  ) : (
                    <Tag>{roleLabel[u.role]}</Tag>
                  )}
                </div>
              </div>
            );
          })}
        </Card>
      </section>

      {canManage && invites.length > 0 && (
        <section className="flex flex-col gap-3">
          <h2 className="h3">Pending invites</h2>
          <Card className="divide-y divide-border">
            {invites.map((i) => (
              <div key={i.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
                <div className="flex min-w-0 flex-col">
                  <span className="truncate text-[14px] font-medium text-ink">{i.email}</span>
                  <span className="body-sm text-ink-muted">
                    {roleLabel[i.role]} · expires {new Date(i.expires_at).toLocaleDateString()}
                  </span>
                </div>
                <Button onClick={() => act(() => api.deleteInvite(i.id))}>Cancel invite</Button>
              </div>
            ))}
          </Card>
        </section>
      )}
    </div>
  );
}

function InviteForm({ onInvited }: { onInvited: (email: string, token: string) => void }) {
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<Invite["role"]>("member");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await api.invite(email.trim(), role);
      onInvited(res.invite.email, res.token);
      setEmail("");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card className="p-4">
      <form onSubmit={submit} className="flex flex-col gap-3">
        <h2 className="h3">Invite someone</h2>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="flex min-w-0 flex-1 flex-col">
            <Field label="Email">
              <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoComplete="off" />
            </Field>
          </div>
          <Field label="Role">
            <select
              value={role}
              onChange={(e) => setRole(e.target.value as Invite["role"])}
              className="h-8 rounded-sm border border-border-strong bg-surface-sunken px-2 text-[14px] text-ink"
            >
              <option value="member">Member</option>
              <option value="admin">Admin: also manages the team</option>
            </select>
          </Field>
          <Button variant="primary" type="submit" disabled={busy || !email.trim()}>
            {busy ? "Creating link…" : "Create invite link"}
          </Button>
        </div>
        {error && <Notice tone="danger">{error}</Notice>}
      </form>
    </Card>
  );
}
