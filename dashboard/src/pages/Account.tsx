import { useState, type FormEvent } from "react";
import { api, signOut, type Me, type User } from "../api";
import { Button, Card, Field, Input, Notice, PageHeader } from "../ui";

// Account is the signed-in person's own name, password and sessions.
export function Account({ me, onChanged, onSignOut }: { me: Me; onChanged: (u: User) => void; onSignOut: () => Promise<void> }) {
  if (!me.user) {
    return (
      <div className="flex max-w-3xl flex-col gap-6">
        <PageHeader title="Account" subtitle="You are signed in with the install's API token, which has the owner's rights." />
        <Card className="flex flex-col gap-3 p-4">
          <p className="body-sm text-ink">
            The API token has no profile or password. People sign in with their own accounts and are invited to workspaces under Members.
          </p>
          <div>
            <Button onClick={() => void onSignOut()}>Sign out</Button>
          </div>
        </Card>
      </div>
    );
  }
  return (
    <div className="flex max-w-3xl flex-col gap-8">
      <PageHeader title="Account" subtitle={`${me.user.email}${me.user.role === "owner" ? " · Platform owner" : me.user.role === "admin" ? " · Platform admin" : ""}`} />
      <Profile user={me.user} onChanged={onChanged} />
      <Password />
      <Sessions onSignOut={onSignOut} />
    </div>
  );
}

function Profile({ user, onChanged }: { user: User; onChanged: (u: User) => void }) {
  const [name, setName] = useState(user.name);
  const [state, setState] = useState<{ busy?: boolean; saved?: boolean; error?: string }>({});

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setState({ busy: true });
    try {
      onChanged(await api.updateMe(name));
      setState({ saved: true });
    } catch (err) {
      setState({ error: (err as Error).message });
    }
  };

  return (
    <Card className="p-4">
      <form onSubmit={submit} className="flex flex-col gap-3">
        <h2 className="h3">Profile</h2>
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" />
        </Field>
        {state.error && <Notice tone="danger">{state.error}</Notice>}
        <div className="flex items-center gap-3">
          <Button type="submit" disabled={state.busy || name === user.name}>
            Save
          </Button>
          {state.saved && <span className="body-sm text-success">Saved.</span>}
        </div>
      </form>
    </Card>
  );
}

function Password() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [state, setState] = useState<{ busy?: boolean; saved?: boolean; error?: string }>({});

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setState({ busy: true });
    try {
      await api.changePassword(current, next);
      setCurrent("");
      setNext("");
      setState({ saved: true });
    } catch (err) {
      setState({ error: (err as Error).message });
    }
  };

  return (
    <Card className="p-4">
      <form onSubmit={submit} className="flex flex-col gap-3">
        <h2 className="h3">Password</h2>
        {/* Lets password managers tie the new password to the account. */}
        <input type="text" autoComplete="username" hidden readOnly />
        <Field label="Current password">
          <Input type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
        </Field>
        <Field label="New password" hint="At least 8 characters. Your other sessions are signed out.">
          <Input type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} required minLength={8} />
        </Field>
        {state.error && <Notice tone="danger">{state.error}</Notice>}
        <div className="flex items-center gap-3">
          <Button type="submit" disabled={state.busy || !current || next.length < 8}>
            Change password
          </Button>
          {state.saved && <span className="body-sm text-success">Password changed.</span>}
        </div>
      </form>
    </Card>
  );
}

function Sessions({ onSignOut }: { onSignOut: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  return (
    <Card className="flex flex-col gap-3 p-4">
      <h2 className="h3">Sessions</h2>
      <p className="body-sm text-ink-muted">Sign out here, or everywhere you are signed in, for example after using a shared computer.</p>
      <div className="flex flex-wrap gap-2">
        <Button onClick={() => void onSignOut()}>Sign out</Button>
        <Button
          variant="danger"
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            await signOut("global");
            await onSignOut();
          }}
        >
          Sign out everywhere
        </Button>
      </div>
    </Card>
  );
}
