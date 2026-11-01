import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { api, ApiError, setToken } from "../api";
import { Button, Card, Field, Input, Logo, Notice, ThemeSwitch } from "../ui";

function Frame({ title, subtitle, children }: { title: string; subtitle: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center px-4 py-8">
      <Card className="w-full max-w-sm p-6">
        <div className="flex flex-col gap-6">
          <div className="flex items-center justify-between">
            <Logo />
            <ThemeSwitch />
          </div>
          <div className="flex flex-col gap-1">
            <h1 className="h2">{title}</h1>
            <p className="body-sm text-ink-muted">{subtitle}</p>
          </div>
          {children}
        </div>
      </Card>
    </div>
  );
}

function message(err: unknown, fallback: string) {
  return err instanceof ApiError ? err.message : fallback;
}

// Login signs in with email and password. In the cloud anyone can create
// an account, which comes with a workspace of its own; a self-hosted
// FloBase also takes the install's API token, and a new one without
// accounts shows Setup instead.
export function Login({
  onSignedIn,
  setupRequired,
  signup,
}: {
  onSignedIn: () => void;
  setupRequired: boolean;
  signup: boolean;
}) {
  const [view, setView] = useState<"signin" | "signup" | "token">("signin");
  if (setupRequired) return <Setup onSignedIn={onSignedIn} />;
  if (view === "token") return <TokenLogin onSignedIn={onSignedIn} onBack={() => setView("signin")} />;
  if (view === "signup" && signup) return <SignUp onSignedIn={onSignedIn} onBack={() => setView("signin")} />;
  return (
    <PasswordLogin
      onSignedIn={onSignedIn}
      onUseToken={signup ? undefined : () => setView("token")}
      onSignUp={signup ? () => setView("signup") : undefined}
    />
  );
}

function SignUp({ onSignedIn, onBack }: { onSignedIn: () => void; onBack: () => void }) {
  const [form, setForm] = useState({
    name: "",
    email: "",
    password: "",
    workspace: "",
  });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.signUp({ ...form, email: form.email.trim() });
      onSignedIn();
    } catch (err) {
      setError(message(err, "Cannot reach the control plane."));
      setBusy(false);
    }
  };

  return (
    <Frame title="Create your account" subtitle="You get a workspace of your own; invite your team to it later.">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="Name">
          <Input autoComplete="name" value={form.name} onChange={set("name")} autoFocus />
        </Field>
        <Field label="Email">
          <Input type="email" autoComplete="username" value={form.email} onChange={set("email")} required />
        </Field>
        <Field label="Password" hint="At least 8 characters.">
          <Input
            type="password"
            autoComplete="new-password"
            value={form.password}
            onChange={set("password")}
            required
            minLength={8}
          />
        </Field>
        <Field label="Organization" hint="The workspace's name, such as your company. You can change it later.">
          <Input value={form.workspace} onChange={set("workspace")} maxLength={64} placeholder="Acme Inc." />
        </Field>
        {error && <Notice tone="danger">{error}</Notice>}
        <Button variant="primary" type="submit" disabled={busy}>
          {busy ? "Creating…" : "Create account"}
        </Button>
      </form>
      <button type="button" onClick={onBack} className="body-sm self-start text-brand underline-offset-2 hover:underline">
        I have an account: sign in
      </button>
    </Frame>
  );
}

function PasswordLogin({
  onSignedIn,
  onUseToken,
  onSignUp,
}: {
  onSignedIn: () => void;
  onUseToken?: () => void;
  onSignUp?: () => void;
}) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.signIn(email.trim(), password);
      onSignedIn();
    } catch (err) {
      setError(message(err, "Cannot reach the control plane."));
      setBusy(false);
    }
  };

  return (
    <Frame title="Sign in" subtitle="Use your FloBase account.">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="Email">
          <Input
            type="email"
            autoComplete="username"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            autoFocus
          />
        </Field>
        <Field label="Password">
          <Input
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </Field>
        {error && <Notice tone="danger">{error}</Notice>}
        <Button variant="primary" type="submit" disabled={busy || !email.trim() || !password}>
          {busy ? "Signing in…" : "Sign in"}
        </Button>
      </form>
      {onSignUp && (
        <button type="button" onClick={onSignUp} className="body-sm self-start text-brand underline-offset-2 hover:underline">
          New here? Create an account
        </button>
      )}
      {onUseToken && (
        <button type="button" onClick={onUseToken} className="body-sm self-start text-brand underline-offset-2 hover:underline">
          Sign in with the API token instead
        </button>
      )}
    </Frame>
  );
}

function TokenLogin({ onSignedIn, onBack }: { onSignedIn: () => void; onBack: () => void }) {
  const [token, setValue] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    setToken(token.trim());
    try {
      await api.me();
      onSignedIn();
    } catch (err) {
      setToken("");
      setError(err instanceof ApiError && err.status === 401 ? "That token is not right." : "Cannot reach the control plane.");
      setBusy(false);
    }
  };

  return (
    <Frame title="Sign in with the API token" subtitle="The token set in FLOBASE_API_TOKEN, with the owner's rights.">
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="API token">
          <Input type="password" autoComplete="off" value={token} onChange={(e) => setValue(e.target.value)} required autoFocus />
        </Field>
        {error && <Notice tone="danger">{error}</Notice>}
        <Button variant="primary" type="submit" disabled={busy || !token.trim()}>
          {busy ? "Signing in…" : "Sign in"}
        </Button>
      </form>
      <button type="button" onClick={onBack} className="body-sm self-start text-brand underline-offset-2 hover:underline">
        Sign in with email instead
      </button>
    </Frame>
  );
}

// Setup creates the owner account. The API token proves the person runs
// this install, so nobody else can claim a new public server first.
function Setup({ onSignedIn }: { onSignedIn: () => void }) {
  const [form, setForm] = useState({
    api_token: "",
    email: "",
    name: "",
    password: "",
  });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.setupOwner({
        ...form,
        api_token: form.api_token.trim(),
        email: form.email.trim(),
      });
      onSignedIn();
    } catch (err) {
      setError(message(err, "Cannot reach the control plane."));
      setBusy(false);
    }
  };

  return (
    <Frame
      title="Create the owner account"
      subtitle="This FloBase has no accounts yet. The owner can invite the rest of the team."
    >
      <form onSubmit={submit} className="flex flex-col gap-4">
        <Field label="API token" hint="FLOBASE_API_TOKEN in the install's .env. It proves you run this server.">
          <Input type="password" autoComplete="off" value={form.api_token} onChange={set("api_token")} required autoFocus />
        </Field>
        <Field label="Name">
          <Input autoComplete="name" value={form.name} onChange={set("name")} />
        </Field>
        <Field label="Email">
          <Input type="email" autoComplete="username" value={form.email} onChange={set("email")} required />
        </Field>
        <Field label="Password" hint="At least 8 characters.">
          <Input
            type="password"
            autoComplete="new-password"
            value={form.password}
            onChange={set("password")}
            required
            minLength={8}
          />
        </Field>
        {error && <Notice tone="danger">{error}</Notice>}
        <Button variant="primary" type="submit" disabled={busy}>
          {busy ? "Creating…" : "Create account"}
        </Button>
      </form>
    </Frame>
  );
}

// AcceptInvite is where an invite link lands: someone new picks a name and
// password; someone with an account enters their password. Either way they
// join the workspace and are signed in.
export function AcceptInvite({ token, onSignedIn }: { token: string; onSignedIn: () => void }) {
  const [invite, setInvite] = useState<{
    email: string;
    role: string;
    workspace: string;
    has_account: boolean;
  } | null>(null);
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api
      .inviteInfo(token)
      .then(setInvite)
      .catch((err) => setError(message(err, "Cannot reach the control plane.")));
  }, [token]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.acceptInvite(token, name, password);
      window.location.hash = "#/";
      onSignedIn();
    } catch (err) {
      setError(message(err, "Cannot reach the control plane."));
      setBusy(false);
    }
  };

  return (
    <Frame
      title={invite ? `Join ${invite.workspace}` : "Join FloBase"}
      subtitle={
        invite
          ? `You were invited as ${invite.role === "admin" ? "an admin" : "a member"}.${invite.has_account ? " Sign in with your FloBase password to accept." : ""}`
          : "Checking the invite…"
      }
    >
      {invite && (
        <form onSubmit={submit} className="flex flex-col gap-4">
          <Field label="Email">
            <Input type="email" autoComplete="username" value={invite.email} readOnly />
          </Field>
          {!invite.has_account && (
            <Field label="Name">
              <Input autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
            </Field>
          )}
          <Field label="Password" hint={invite.has_account ? undefined : "At least 8 characters."}>
            <Input
              type="password"
              autoComplete={invite.has_account ? "current-password" : "new-password"}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              minLength={8}
            />
          </Field>
          {error && <Notice tone="danger">{error}</Notice>}
          <Button variant="primary" type="submit" disabled={busy}>
            {busy ? "Joining…" : invite.has_account ? "Join workspace" : "Create account"}
          </Button>
        </form>
      )}
      {!invite && error && <Notice tone="danger">{error}</Notice>}
    </Frame>
  );
}
