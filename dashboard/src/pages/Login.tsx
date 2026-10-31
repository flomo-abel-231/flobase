import { useState, type FormEvent } from "react";
import { api, ApiError, setToken } from "../api";
import { Button, Card, Field, Input, Logo, Notice, ThemeSwitch } from "../ui";

export function Login({ onSignedIn }: { onSignedIn: () => void }) {
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
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center px-4">
      <Card className="w-full max-w-sm p-6">
        <form onSubmit={submit} className="flex flex-col gap-6">
          <div className="flex items-center justify-between">
            <Logo />
            <ThemeSwitch />
          </div>
          <div className="flex flex-col gap-1">
            <h1 className="h2">Sign in</h1>
            <p className="body-sm text-ink-muted">Use the API token set in FLOBASE_API_TOKEN.</p>
          </div>
          <Field label="API token">
            <Input
              type="password"
              autoComplete="current-password"
              value={token}
              onChange={(e) => setValue(e.target.value)}
              required
              autoFocus
            />
          </Field>
          {error && <Notice tone="danger">{error}</Notice>}
          <Button variant="primary" type="submit" disabled={busy || !token.trim()}>
            {busy ? "Signing in…" : "Sign in"}
          </Button>
        </form>
      </Card>
    </div>
  );
}
