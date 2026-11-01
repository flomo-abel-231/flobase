import { useEffect, useState, type FormEvent } from "react";
import { api, type Automation, type AutomationInput, type LLMProvider } from "../api";
import { Button, Card, Field, Input, Notice, PageHeader } from "../ui";
import { navigate } from "../App";

// Automations run the assistant on a prompt every hour, day or week. Each run
// is a conversation, linked from its row.

const everyLabel: Record<Automation["every"], string> = { hour: "Every hour", day: "Every day", week: "Every week" };

const examples = [
  { name: "Backup check", every: "day" as const, prompt: "Check every Postgres project's backups. Report any project whose latest full backup is older than 26 hours, or whose off-site copy is failing." },
  { name: "Slow queries", every: "week" as const, prompt: "For each Postgres project, list the 5 slowest queries from pg_stat_statements if the extension is on, and suggest an index for each where one would help." },
  { name: "Failed projects", every: "hour" as const, prompt: "List any project whose status is failed, and read the last lines of its db logs to say why." },
];

export function Automations() {
  const [list, setList] = useState<Automation[] | null>(null);
  const [providers, setProviders] = useState<LLMProvider[]>([]);
  const [editing, setEditing] = useState<Automation | "new" | null>(null);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState<string | null>(null);

  const load = async () => {
    try {
      const [a, s] = await Promise.all([api.automations(), api.assistantSettings()]);
      setList(a);
      setProviders(s.providers);
    } catch (e) {
      setError((e as Error).message);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
      setConfirm(null);
      await load();
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const runNow = async (a: Automation) => {
    try {
      const c = await api.runAutomation(a.id);
      navigate(`/assistant/${c.id}`);
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const toInput = (a: Automation, patch: Partial<AutomationInput> = {}): AutomationInput => ({
    name: a.name,
    prompt: a.prompt,
    provider_id: a.provider_id ?? "",
    every: a.every,
    enabled: a.enabled,
    ...patch,
  });

  const modelName = (id: string | null) => providers.find((p) => p.id === id)?.name ?? "Default model";

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Automations"
        action={
          providers.length > 0 && editing === null ? (
            <Button variant="primary" onClick={() => setEditing("new")}>
              + Add
            </Button>
          ) : undefined
        }
        subtitle={
          <>
            Run the assistant on a schedule. Nobody is there to approve, so runs only do what the{" "}
            <a href="#/assistant/settings" className="text-brand underline-offset-2 hover:underline">
              permission setting
            </a>{" "}
            allows without asking.
          </>
        }
      />
      {error && <Notice tone="danger">{error}</Notice>}

      {list && providers.length === 0 && (
        <Card className="flex flex-col items-start gap-3 p-5">
          <span className="text-[14px] font-semibold text-ink">Add a model first</span>
          <span className="body-sm text-ink-muted">Automations use the assistant, which needs a model and your API key.</span>
          <Button variant="primary" onClick={() => navigate("/assistant/settings")}>
            Add a model
          </Button>
        </Card>
      )}

      {editing !== null && (
        <AutomationForm
          initial={editing === "new" ? null : editing}
          providers={providers}
          onCancel={() => setEditing(null)}
          onSaved={async () => {
            setEditing(null);
            await load();
          }}
        />
      )}

      {list && list.length === 0 && providers.length > 0 && editing === null && (
        <Card className="flex flex-col gap-3 p-5">
          <span className="text-[14px] font-semibold text-ink">No automations yet</span>
          <span className="body-sm text-ink-muted">Some ideas:</span>
          <div className="grid gap-2 sm:grid-cols-3">
            {examples.map((ex) => (
              <button
                key={ex.name}
                type="button"
                onClick={() =>
                  act(() => api.createAutomation({ name: ex.name, prompt: ex.prompt, every: ex.every, provider_id: "" }))
                }
                className="flex flex-col gap-1 rounded-md border border-border bg-surface-raised px-3 py-2 text-left transition hover:border-border-strong hover:bg-surface-sunken"
              >
                <span className="text-[13px] font-semibold text-ink">
                  {ex.name} · {everyLabel[ex.every].toLowerCase()}
                </span>
                <span className="body-sm text-ink-muted">{ex.prompt}</span>
              </button>
            ))}
          </div>
        </Card>
      )}

      {list && list.length > 0 && (
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full border-collapse text-left">
            <thead className="bg-surface-sunken">
              <tr>
                {["Automation", "Runs", "Last run", ""].map((h) => (
                  <th key={h} className="label whitespace-nowrap border-b border-border px-3 py-2 text-ink-muted">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {list.map((a) => (
                <tr key={a.id} className="border-b border-border last:border-0">
                  <td className="max-w-md px-3 py-2">
                    <div className="text-[14px] font-semibold text-ink">{a.name}</div>
                    <div className="body-sm line-clamp-2 text-ink-muted">{a.prompt}</div>
                  </td>
                  <td className="body-sm whitespace-nowrap px-3 py-2 text-ink-muted">
                    {a.enabled ? everyLabel[a.every] : <span className="text-warning">Paused</span>}
                    <div>{modelName(a.provider_id)}</div>
                    {a.enabled && <div>Next {new Date(a.next_run_at).toLocaleString()}</div>}
                  </td>
                  <td className="body-sm whitespace-nowrap px-3 py-2">
                    {a.last_run_at && a.last_chat_id ? (
                      <a href={`#/assistant/${a.last_chat_id}`} className="text-brand underline-offset-2 hover:underline">
                        {new Date(a.last_run_at).toLocaleString()}
                      </a>
                    ) : (
                      <span className="text-ink-muted">Not yet</span>
                    )}
                    {a.last_status === "failed" && <div className="text-danger">Failed</div>}
                    {a.last_status === "running" && <div className="text-brand">Running</div>}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-right">
                    <span className="inline-flex gap-2">
                      <Button onClick={() => runNow(a)}>Run now</Button>
                      <Button onClick={() => setEditing(a)}>Edit</Button>
                      <Button onClick={() => act(() => api.updateAutomation(a.id, toInput(a, { enabled: !a.enabled })))}>
                        {a.enabled ? "Pause" : "Resume"}
                      </Button>
                      {confirm === a.id ? (
                        <>
                          <Button variant="danger" onClick={() => act(() => api.deleteAutomation(a.id))}>
                            Remove
                          </Button>
                          <Button onClick={() => setConfirm(null)}>Cancel</Button>
                        </>
                      ) : (
                        <Button onClick={() => setConfirm(a.id)} aria-label={`Remove ${a.name}`}>
                          Remove
                        </Button>
                      )}
                    </span>
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

function AutomationForm({
  initial,
  providers,
  onCancel,
  onSaved,
}: {
  initial: Automation | null;
  providers: LLMProvider[];
  onCancel: () => void;
  onSaved: () => void;
}) {
  const [f, setF] = useState<AutomationInput>({
    name: initial?.name ?? "",
    prompt: initial?.prompt ?? "",
    provider_id: initial?.provider_id ?? "",
    every: initial?.every ?? "day",
    enabled: initial?.enabled ?? true,
  });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      if (initial) await api.updateAutomation(initial.id, f);
      else await api.createAutomation(f);
      onSaved();
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  const select = "h-8 rounded-sm border border-border-strong bg-surface-sunken px-2 text-[14px] text-ink";

  return (
    <Card className="p-5">
      <form onSubmit={save} className="flex flex-col gap-4">
        <h3 className="text-[14px] font-semibold text-ink">{initial ? `Edit ${initial.name}` : "Add an automation"}</h3>
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label="Name">
            <Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} required maxLength={64} placeholder="Backup check" />
          </Field>
          <Field label="Runs">
            <select value={f.every} onChange={(e) => setF({ ...f, every: e.target.value as Automation["every"] })} className={select}>
              <option value="hour">Every hour</option>
              <option value="day">Every day</option>
              <option value="week">Every week</option>
            </select>
          </Field>
          <Field label="Model">
            <select value={f.provider_id} onChange={(e) => setF({ ...f, provider_id: e.target.value })} className={select}>
              <option value="">Default model</option>
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <Field label="What should it do?" hint="Written as you would ask the assistant. Each run starts fresh.">
          <textarea
            value={f.prompt}
            onChange={(e) => setF({ ...f, prompt: e.target.value })}
            required
            rows={4}
            className="rounded-sm border border-border-strong bg-surface-sunken px-2.5 py-1.5 text-[14px] text-ink placeholder:text-ink-muted"
            placeholder="Check every project's backups and report any older than a day."
          />
        </Field>
        {error && <Notice tone="danger">{error}</Notice>}
        <div className="flex gap-2">
          <Button variant="primary" type="submit" disabled={busy}>
            {busy ? "Saving…" : "Save"}
          </Button>
          <Button type="button" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
        </div>
      </form>
    </Card>
  );
}
