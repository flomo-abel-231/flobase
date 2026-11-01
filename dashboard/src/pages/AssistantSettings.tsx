import { useEffect, useState, type FormEvent } from "react";
import { api, type AssistantMode, type LLMProvider, type ProviderInput } from "../api";
import { Button, Card, Field, Input, Notice, PageHeader } from "../ui";

// Models people add with their own API keys, and what the assistant may do
// without asking. Keys never come back from the server, only their last
// characters.

type Preset = {
  id: string;
  label: string;
  kind: ProviderInput["kind"];
  baseURL: string;
  model: string;
  keyHint: string;
  noKey?: boolean;
};

const presets: Preset[] = [
  { id: "anthropic", label: "Anthropic (Claude)", kind: "anthropic", baseURL: "", model: "claude-opus-5-5", keyHint: "From console.anthropic.com → API keys." },
  { id: "openai", label: "OpenAI", kind: "openai", baseURL: "https://api.openai.com/v1", model: "", keyHint: "From platform.openai.com → API keys." },
  { id: "gemini", label: "Google Gemini", kind: "openai", baseURL: "https://generativelanguage.googleapis.com/v1beta/openai", model: "", keyHint: "From Google AI Studio → Get API key." },
  { id: "openrouter", label: "OpenRouter", kind: "openai", baseURL: "https://openrouter.ai/api/v1", model: "", keyHint: "From openrouter.ai → Keys. One key, many models." },
  { id: "groq", label: "Groq", kind: "openai", baseURL: "https://api.groq.com/openai/v1", model: "", keyHint: "From console.groq.com → API keys." },
  { id: "mistral", label: "Mistral", kind: "openai", baseURL: "https://api.mistral.ai/v1", model: "", keyHint: "From console.mistral.ai → API keys." },
  { id: "deepseek", label: "DeepSeek", kind: "openai", baseURL: "https://api.deepseek.com/v1", model: "", keyHint: "From platform.deepseek.com → API keys." },
  { id: "xai", label: "xAI (Grok)", kind: "openai", baseURL: "https://api.x.ai/v1", model: "", keyHint: "From console.x.ai → API keys." },
  {
    id: "ollama",
    label: "Ollama (on your machine)",
    kind: "openai",
    baseURL: "http://host.docker.internal:11434/v1",
    model: "",
    keyHint: "No key needed. FloBase runs in Docker, so it reaches your machine as host.docker.internal.",
    noKey: true,
  },
  { id: "custom", label: "Other OpenAI-compatible service", kind: "openai", baseURL: "", model: "", keyHint: "Any service with an OpenAI-style /chat/completions API and tool calling." },
];

const presetLabel = (id: string) => presets.find((p) => p.id === id)?.label ?? id;

const modes: { id: AssistantMode; title: string; text: string }[] = [
  { id: "read", title: "Read only", text: "Looks at projects, tables, logs and backups, and runs read-only SQL. Changes nothing." },
  { id: "ask", title: "Ask before changes", text: "Reads at once. Every change, such as SQL that writes or a new project, waits for your approval." },
  { id: "auto", title: "Ask before deletes", text: "Reads and changes at once. Deleting a project, a user or data waits for your approval." },
];

export function AssistantSettings() {
  const [mode, setMode] = useState<AssistantMode | null>(null);
  const [providers, setProviders] = useState<LLMProvider[]>([]);
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState<string | null>(null);

  const load = async () => {
    try {
      const s = await api.assistantSettings();
      setMode(s.mode);
      setProviders(s.providers);
      if (s.providers.length === 0) setAdding(true);
    } catch (e) {
      setError((e as Error).message);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const chooseMode = async (m: AssistantMode) => {
    const before = mode;
    setMode(m);
    try {
      await api.setAssistantMode(m);
    } catch (e) {
      setMode(before);
      setError((e as Error).message);
    }
  };

  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
      setConfirm(null);
      await load();
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <div className="flex flex-col gap-8">
      <PageHeader
        title="Models & permissions"
        subtitle={
          <>
            <a href="#/assistant" className="text-brand underline-offset-2 hover:underline">
              Assistant
            </a>{" "}
            uses models you add with your own API keys. Keys stay on this server.
          </>
        }
      />
      {error && <Notice tone="danger">{error}</Notice>}

      <section className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <h2 className="h3">What the assistant may do without asking</h2>
          <p className="body-sm text-ink-muted">Automations follow the same setting, and skip anything that would need your approval.</p>
        </div>
        <div role="radiogroup" aria-label="Permission" className="grid gap-2 sm:grid-cols-3">
          {modes.map((m) => (
            <button
              key={m.id}
              type="button"
              role="radio"
              aria-checked={mode === m.id}
              onClick={() => chooseMode(m.id)}
              className={`flex flex-col gap-1 rounded-md border px-3 py-2.5 text-left transition ${
                mode === m.id ? "border-brand bg-brand-subtle" : "border-border bg-surface-raised hover:border-border-strong"
              }`}
            >
              <span className="text-[14px] font-semibold text-ink">{m.title}</span>
              <span className="body-sm text-ink-muted">{m.text}</span>
            </button>
          ))}
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <h2 className="h3">Models</h2>
          {!adding && (
            <Button variant="primary" onClick={() => setAdding(true)}>
              + Add
            </Button>
          )}
        </div>
        <p className="body-sm text-ink-muted">
          New conversations use the default model; you can pick another when you start one. What you send, and what the assistant
          reads from your projects, goes to the model's service.
        </p>

        {adding && (
          <AddModel
            first={providers.length === 0}
            onCancel={() => setAdding(false)}
            onSaved={async () => {
              setAdding(false);
              await load();
            }}
          />
        )}

        {providers.length > 0 && (
          <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
            <table className="w-full border-collapse text-left">
              <thead className="bg-surface-sunken">
                <tr>
                  {["Name", "Model", "Key", ""].map((h) => (
                    <th key={h} className="label whitespace-nowrap border-b border-border px-3 py-2 text-ink-muted">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {providers.map((p) => (
                  <tr key={p.id} className="border-b border-border last:border-0">
                    <td className="px-3 py-2">
                      <div className="flex items-center gap-2">
                        <span className="text-[14px] font-semibold text-ink">{p.name}</span>
                        {p.default && (
                          <span className="inline-flex items-center gap-1.5 label text-success">
                            <span className="size-2 rounded-full bg-success" aria-hidden />
                            Default
                          </span>
                        )}
                      </div>
                      <div className="body-sm text-ink-muted">{presetLabel(p.preset)}</div>
                    </td>
                    <td className="code px-3 py-2 text-ink">{p.model}</td>
                    <td className="code whitespace-nowrap px-3 py-2 text-ink-muted">{p.api_key_hint || "none"}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">
                      <span className="inline-flex gap-2">
                        {!p.default && <Button onClick={() => act(() => api.defaultProvider(p.id))}>Make default</Button>}
                        {confirm === p.id ? (
                          <>
                            <Button variant="danger" onClick={() => act(() => api.deleteProvider(p.id))}>
                              Remove
                            </Button>
                            <Button onClick={() => setConfirm(null)}>Cancel</Button>
                          </>
                        ) : (
                          <Button onClick={() => setConfirm(p.id)} aria-label={`Remove ${p.name}`}>
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
        {confirm && <p className="body-sm text-ink-muted">Conversations with this model are kept, but cannot continue.</p>}
      </section>
    </div>
  );
}

function AddModel({ first, onCancel, onSaved }: { first: boolean; onCancel: () => void; onSaved: () => void }) {
  const [presetId, setPresetId] = useState("anthropic");
  const preset = presets.find((p) => p.id === presetId) ?? presets[0];
  const [f, setF] = useState<ProviderInput>({ name: "", kind: "anthropic", preset: "anthropic", base_url: "", api_key: "", model: "claude-opus-5-5" });
  const [models, setModels] = useState<string[]>([]);
  const [status, setStatus] = useState<{ tone: "danger" | "ok"; text: string } | null>(null);
  const [busy, setBusy] = useState<"" | "models" | "test" | "save">("");

  const set = (patch: Partial<ProviderInput>) => {
    setF((cur) => ({ ...cur, ...patch }));
    setStatus(null);
  };

  const pick = (id: string) => {
    const p = presets.find((x) => x.id === id) ?? presets[0];
    setPresetId(p.id);
    setModels([]);
    set({ kind: p.kind, preset: p.id, base_url: p.baseURL, model: p.model, api_key: "" });
  };

  const run = async (kind: "models" | "test" | "save", fn: () => Promise<void>) => {
    setBusy(kind);
    try {
      await fn();
    } catch (e) {
      setStatus({ tone: "danger", text: (e as Error).message });
    } finally {
      setBusy("");
    }
  };

  const loadModels = () =>
    run("models", async () => {
      const res = await api.providerModels(f);
      setModels(res.models);
      if (!f.model && res.models.length > 0) set({ model: res.models[0] });
      setStatus({ tone: "ok", text: `${res.models.length} models available with this key.` });
    });

  const test = () =>
    run("test", async () => {
      await api.testProvider(f);
      setStatus({ tone: "ok", text: `Connected: ${f.model} answered.` });
    });

  const save = (e: FormEvent) => {
    e.preventDefault();
    run("save", async () => {
      await api.createProvider(f);
      onSaved();
    });
  };

  const showURL = preset.kind === "openai";

  return (
    <Card className="p-5">
      <form onSubmit={save} className="flex flex-col gap-4">
        <h3 className="text-[14px] font-semibold text-ink">{first ? "Add your first model" : "Add a model"}</h3>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Provider">
            <select
              value={presetId}
              onChange={(e) => pick(e.target.value)}
              className="h-8 rounded-sm border border-border-strong bg-surface-sunken px-2 text-[14px] text-ink"
            >
              {presets.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.label}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Name" hint="Optional. Defaults to the model's name.">
            <Input value={f.name} onChange={(e) => set({ name: e.target.value })} maxLength={64} placeholder={`e.g. ${preset.label.split(" (")[0]} for ops`} />
          </Field>
          {showURL && (
            <Field label="Base URL">
              <Input value={f.base_url} onChange={(e) => set({ base_url: e.target.value })} placeholder="https://api.example.com/v1" required={presetId === "custom"} />
            </Field>
          )}
          <Field label={preset.noKey ? "API key (optional)" : "API key"} hint={preset.keyHint}>
            <Input
              type="password"
              value={f.api_key}
              onChange={(e) => set({ api_key: e.target.value })}
              required={!preset.noKey}
              autoComplete="new-password"
              spellCheck={false}
            />
          </Field>
          <Field label="Model" hint={models.length ? "Pick from the list or type a model id." : "Type a model id, or load the list with your key."}>
            <div className="flex gap-2">
              <div className="flex min-w-0 flex-1 flex-col">
                <Input value={f.model} onChange={(e) => set({ model: e.target.value })} list="model-list" required spellCheck={false} />
                <datalist id="model-list">
                  {models.map((m) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
              </div>
              <Button type="button" onClick={loadModels} disabled={busy !== "" || (!f.api_key && !preset.noKey)}>
                {busy === "models" ? "Loading…" : "Load models"}
              </Button>
            </div>
          </Field>
        </div>

        {status &&
          (status.tone === "ok" ? (
            <p className="body-sm flex items-center gap-2 text-success">
              <span className="size-2 rounded-full bg-success" aria-hidden />
              {status.text}
            </p>
          ) : (
            <Notice tone="danger">{status.text}</Notice>
          ))}

        <div className="flex flex-wrap gap-2">
          <Button variant="primary" type="submit" disabled={busy !== ""}>
            {busy === "save" ? "Testing and saving…" : "Save"}
          </Button>
          <Button type="button" onClick={test} disabled={busy !== "" || !f.model}>
            {busy === "test" ? "Testing…" : "Test"}
          </Button>
          {!first && (
            <Button type="button" onClick={onCancel} disabled={busy !== ""}>
              Cancel
            </Button>
          )}
        </div>
      </form>
    </Card>
  );
}
