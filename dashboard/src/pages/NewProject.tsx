import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { api, formatMB, formatPrice, type Catalog, type EngineId } from "../api";
import { Button, Card, Field, Input, Notice } from "../ui";
import { navigate } from "../App";

function Choice({
  name,
  value,
  checked,
  disabled,
  onChange,
  children,
}: {
  name: string;
  value: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (v: string) => void;
  children: ReactNode;
}) {
  return (
    <label
      className={`flex cursor-pointer flex-col gap-1 rounded-md border p-3 transition has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-brand ${
        checked ? "border-brand bg-brand-subtle" : "border-border-strong bg-surface-raised hover:bg-surface-sunken"
      } ${disabled ? "cursor-not-allowed opacity-50" : ""}`}
    >
      <input
        type="radio"
        name={name}
        value={value}
        checked={checked}
        disabled={disabled}
        onChange={() => onChange(value)}
        className="sr-only"
      />
      {children}
    </label>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <fieldset className="flex flex-col gap-3">
      <legend className="mb-2 text-[14px] font-semibold text-ink">{title}</legend>
      {children}
    </fieldset>
  );
}

export function NewProject() {
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [name, setName] = useState("");
  const [engine, setEngine] = useState<EngineId>("postgres");
  const [region, setRegion] = useState("local");
  const [plan, setPlan] = useState("free");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api
      .catalog()
      .then((c) => {
        setCatalog(c);
        // Start on an open region; local may be closed in the cloud.
        if (!c.regions.some((r) => r.id === "local" && r.status === "open")) {
          const open = c.regions.find((r) => r.status === "open");
          if (open) setRegion(open.id);
        }
      })
      .catch((e) => setError(e.message));
  }, []);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const p = await api.createProject({ name: name.trim(), engine, region, plan });
      navigate(`/project/${p.ref}`);
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  if (!catalog) return error ? <Notice tone="danger">{error}</Notice> : null;
  // Closed regions are being set up by the owner; customers do not see them.
  const regions = catalog.regions.filter((r) => r.status !== "closed");
  const prices = catalog.regions.find((r) => r.id === region)?.prices;
  const selectedPlan = catalog.plans.find((p) => p.id === plan);

  return (
    <form onSubmit={submit} className="flex max-w-3xl flex-col gap-8">
      <div className="flex flex-col gap-2">
        <nav aria-label="Breadcrumb" className="body-sm flex items-center gap-1.5 text-ink-muted">
          <a href="#/" className="hover:text-ink">
            Projects
          </a>
          <span aria-hidden>›</span>
          <span className="text-ink">New</span>
        </nav>
        <h1 className="h2">New project</h1>
      </div>

      <Card className="flex flex-col gap-6 p-5">
        <Field label="Project name">
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={64} required autoFocus placeholder="my-app" />
        </Field>

        <Section title="Database engine">
          <div className="grid gap-3 sm:grid-cols-3">
            {catalog.engines.map((e) => (
              <Choice key={e.id} name="engine" value={e.id} checked={engine === e.id} onChange={(v) => setEngine(v as EngineId)}>
                <span className="font-medium text-ink">{e.name}</span>
                <span className="body-sm text-ink-muted">{e.description}</span>
              </Choice>
            ))}
          </div>
        </Section>

        <Section title="Location">
          <div className="grid gap-3 sm:grid-cols-3">
            {regions.map((r) => (
              <Choice
                key={r.id}
                name="region"
                value={r.id}
                checked={region === r.id}
                disabled={r.status !== "open"}
                onChange={setRegion}
              >
                <span className="font-medium text-ink">{r.name}</span>
                <span className="body-sm text-ink-muted">{r.status === "open" ? r.location : "Full"}</span>
              </Choice>
            ))}
          </div>
        </Section>

        <Section title={catalog.billing ? "Plan" : "Size"}>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {catalog.plans.map((p) => (
              <Choice key={p.id} name="plan" value={p.id} checked={plan === p.id} disabled={!p.available} onChange={setPlan}>
                <span className="font-medium text-ink">{p.name}</span>
                {catalog.billing && (
                  <span className="metric text-ink">
                    {formatPrice(prices?.[p.id] ?? p.price_usd)}
                    <span className="body-sm text-ink-muted"> /mo</span>
                  </span>
                )}
                <span className="code text-ink-muted">
                  {p.cpu} vCPU · {p.memory_mb >= 1024 ? `${p.memory_mb / 1024} GB` : `${p.memory_mb} MB`} RAM
                </span>
                <span className="body-sm text-ink-muted">
                  {formatMB(Math.round(p.disk_gb * 1000))} database · {p.backups === "None" ? "no" : p.backups} backups
                </span>
                {!p.available && <span className="body-sm text-ink-muted">Coming soon</span>}
              </Choice>
            ))}
          </div>
          {selectedPlan?.deleted_after_days && (
            <Notice tone="warning">
              Free projects are deleted {selectedPlan.deleted_after_days} days after they are created, together with their data. Export
              anything you want to keep before then; you will get reminders by email and in the dashboard.
            </Notice>
          )}
        </Section>
      </Card>

      {error && <Notice tone="danger">{error}</Notice>}

      <div className="flex justify-end gap-3">
        <Button type="button" onClick={() => navigate("/")}>
          Cancel
        </Button>
        <Button variant="primary" type="submit" disabled={busy || !name.trim()}>
          {busy ? "Creating…" : "Create project"}
        </Button>
      </div>
    </form>
  );
}
