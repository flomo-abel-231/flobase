import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { api, type AssistantMode, type Chat, type ChatEntry, type LLMProvider, type ToolEntry } from "../api";
import { Button, Card, Notice, PageHeader, Tag } from "../ui";
import { navigate } from "../App";
import { Markdown } from "../Markdown";

// The assistant: conversations on the left, the open one on the right.
// Tool calls show as compact cards; the ones that change something wait for
// Approve or Refuse, depending on the permission setting.

const modeLabel: Record<AssistantMode, string> = {
  read: "Read only",
  ask: "Asks before changes",
  auto: "Asks before deletes",
};

export function Assistant({ chatId }: { chatId?: string }) {
  const [providers, setProviders] = useState<LLMProvider[] | null>(null);
  const [mode, setMode] = useState<AssistantMode>("ask");
  const [chats, setChats] = useState<Chat[]>([]);
  const [error, setError] = useState("");

  const loadChats = useCallback(() => {
    api.chats().then(setChats).catch((e) => setError(e.message));
  }, []);

  useEffect(() => {
    api
      .assistantSettings()
      .then((s) => {
        setProviders(s.providers);
        setMode(s.mode);
      })
      .catch((e) => setError(e.message));
    loadChats();
  }, [loadChats]);

  const settingsLink = (
    <a href="#/assistant/settings" className="body-sm text-brand underline-offset-2 hover:underline">
      Models & permissions
    </a>
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Assistant"
        action={providers && providers.length > 0 ? <Tag>{modeLabel[mode]}</Tag> : undefined}
        subtitle={
          <>
            Ask about your projects, or have it run SQL, create projects, take backups and more, using your own model and API key.{" "}
            {settingsLink}
          </>
        }
      />
      {error && <Notice tone="danger">{error}</Notice>}

      {providers && providers.length === 0 && (
        <Card className="flex flex-col items-start gap-3 p-5">
          <span className="text-[14px] font-semibold text-ink">Add a model to start</span>
          <span className="body-sm text-ink-muted">
            The assistant uses a model you choose with your own API key: Claude, OpenAI, Gemini, or any OpenAI-compatible service,
            including Ollama on your own machine.
          </span>
          <Button variant="primary" onClick={() => navigate("/assistant/settings")}>
            Add a model
          </Button>
        </Card>
      )}

      {providers && providers.length > 0 && (
        <div className="grid gap-4 lg:grid-cols-[220px_1fr]">
          <nav aria-label="Conversations" className="flex flex-col gap-1">
            <Button variant="primary" onClick={() => navigate("/assistant")} className="mb-2 justify-start">
              + New conversation
            </Button>
            {chats.length === 0 && <span className="body-sm px-2.5 text-ink-muted">No conversations yet.</span>}
            <div className="flex max-h-[60vh] flex-col gap-0.5 overflow-y-auto">
              {chats.map((c) => (
                <a
                  key={c.id}
                  href={`#/assistant/${c.id}`}
                  aria-current={c.id === chatId ? "page" : undefined}
                  className={`flex items-center gap-2 rounded-sm px-2.5 py-1.5 text-[13px] ${
                    c.id === chatId ? "bg-brand-subtle text-ink" : "text-ink-muted hover:bg-surface-sunken hover:text-ink"
                  }`}
                >
                  {c.status === "waiting" && <span className="size-2 shrink-0 rounded-full bg-warning" title="Waiting for you" />}
                  {c.status === "running" && <span className="size-2 shrink-0 animate-pulse rounded-full bg-brand" title="Working" />}
                  <span className="truncate">{c.automation_id ? "⟳ " : ""}{c.title}</span>
                </a>
              ))}
            </div>
          </nav>
          <Conversation key={chatId ?? "new"} chatId={chatId} providers={providers} onChange={loadChats} />
        </div>
      )}
    </div>
  );
}

function Conversation({ chatId, providers, onChange }: { chatId?: string; providers: LLMProvider[]; onChange: () => void }) {
  const [chat, setChat] = useState<Chat | null>(null);
  const [text, setText] = useState("");
  const [providerId, setProviderId] = useState(providers.find((p) => p.default)?.id ?? providers[0].id);
  const [error, setError] = useState("");
  const [sending, setSending] = useState(false);
  const bottom = useRef<HTMLDivElement>(null);
  const status = chat?.status;

  useEffect(() => {
    if (!chatId) return;
    api.chat(chatId).then(setChat).catch((e) => setError(e.message));
  }, [chatId]);

  // Follow the conversation while the assistant works.
  useEffect(() => {
    if (!chatId || status !== "running") return;
    const t = setInterval(() => {
      api
        .chat(chatId)
        .then((c) => {
          setChat(c);
          if (c.status !== "running") onChange();
        })
        .catch(() => {});
    }, 1200);
    return () => clearInterval(t);
  }, [chatId, status, onChange]);

  const count = chat?.entries?.length ?? 0;
  useEffect(() => {
    bottom.current?.scrollIntoView({ block: "end", behavior: "smooth" });
  }, [count, status]);

  const send = async (e?: FormEvent) => {
    e?.preventDefault();
    const msg = text.trim();
    if (!msg || sending) return;
    setSending(true);
    setError("");
    try {
      if (chat) {
        setChat(await api.sendMessage(chat.id, msg));
      } else {
        const c = await api.createChat(msg, providerId);
        onChange();
        navigate(`/assistant/${c.id}`);
      }
      setText("");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSending(false);
    }
  };

  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      send();
    }
  };

  const decide = async (callId: string, approve: boolean) => {
    if (!chat) return;
    try {
      setChat(await api.decide(chat.id, callId, approve));
      onChange();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const remove = async () => {
    if (!chat) return;
    try {
      await api.deleteChat(chat.id);
      onChange();
      navigate("/assistant");
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const model = providers.find((p) => p.id === (chat?.provider_id ?? providerId));
  const busy = status === "running";

  return (
    <Card className="flex min-h-[60vh] min-w-0 flex-col">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-2.5">
        <span className="min-w-0 truncate text-[14px] font-semibold text-ink">{chat?.title ?? "New conversation"}</span>
        <div className="flex items-center gap-2">
          {chat ? (
            <>
              {model && <span className="body-sm text-ink-muted">{model.name}</span>}
              <Button onClick={remove} disabled={busy} aria-label="Delete conversation">
                Delete
              </Button>
            </>
          ) : (
            <label className="flex items-center gap-2 body-sm text-ink-muted">
              Model
              <select
                value={providerId}
                onChange={(e) => setProviderId(e.target.value)}
                className="h-8 rounded-sm border border-border-strong bg-surface-sunken px-2 text-[13px] text-ink"
              >
                {providers.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>
      </div>

      <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-4 py-4">
        {!chat && !chatId && <Suggestions onPick={setText} />}
        {chat?.entries?.map((e, i) => <Entry key={i} entry={e} onDecide={decide} />)}
        {busy && (
          <p className="body-sm flex items-center gap-2 text-ink-muted">
            <span className="size-2 animate-pulse rounded-full bg-brand" aria-hidden />
            Working…
          </p>
        )}
        {status === "waiting" && <p className="body-sm text-warning">Waiting for you to approve or refuse the actions above.</p>}
        <div ref={bottom} />
      </div>

      {error && (
        <div className="px-4 pb-2">
          <Notice tone="danger">{error}</Notice>
        </div>
      )}
      <form onSubmit={send} className="flex items-end gap-2 border-t border-border p-3">
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKey}
          rows={Math.min(6, Math.max(1, text.split("\n").length))}
          placeholder={busy ? "The assistant is working…" : "Ask or tell the assistant what to do"}
          aria-label="Message"
          disabled={busy}
          className="min-h-8 flex-1 resize-none rounded-sm border border-border-strong bg-surface-sunken px-2.5 py-1.5 text-[14px] text-ink placeholder:text-ink-muted"
        />
        <Button variant="primary" type="submit" disabled={busy || sending || !text.trim()}>
          Send
        </Button>
      </form>
    </Card>
  );
}

const suggestions = [
  "Which projects do I have, and are they all healthy?",
  "Show the 10 largest tables in my Postgres project.",
  "Check every project's backups and tell me if any are older than a day.",
  "Create a todos table with RLS on in my project.",
];

function Suggestions({ onPick }: { onPick: (s: string) => void }) {
  return (
    <div className="flex flex-col gap-2">
      <span className="label text-ink-muted">Try</span>
      <div className="grid gap-2 sm:grid-cols-2">
        {suggestions.map((s) => (
          <button
            key={s}
            type="button"
            onClick={() => onPick(s)}
            className="rounded-md border border-border bg-surface-raised px-3 py-2 text-left body-sm text-ink transition hover:border-border-strong hover:bg-surface-sunken"
          >
            {s}
          </button>
        ))}
      </div>
    </div>
  );
}

function Entry({ entry, onDecide }: { entry: ChatEntry; onDecide: (callId: string, approve: boolean) => void }) {
  if (entry.role === "user") {
    return (
      <div className="ml-auto max-w-[85%] whitespace-pre-wrap rounded-md bg-brand-subtle px-3 py-2 text-[14px] text-ink">{entry.text}</div>
    );
  }
  if (entry.role === "assistant") return <Markdown text={entry.text ?? ""} />;
  if (entry.role === "notice") return <p className="body-sm text-warning">{entry.text}</p>;
  if (entry.tool) return <ToolCard tool={entry.tool} onDecide={onDecide} />;
  return null;
}

const riskTag: Record<ToolEntry["risk"], { label: string; className: string }> = {
  read: { label: "Reads", className: "text-ink-muted" },
  change: { label: "Changes", className: "text-warning" },
  delete: { label: "Deletes", className: "text-danger" },
};

const statusText: Record<ToolEntry["status"], { label: string; className: string }> = {
  running: { label: "Running…", className: "text-brand" },
  approved: { label: "Approved", className: "text-brand" },
  done: { label: "Done", className: "text-success" },
  failed: { label: "Failed", className: "text-danger" },
  waiting: { label: "Needs your approval", className: "text-warning" },
  denied: { label: "Not done", className: "text-ink-muted" },
};

function ToolCard({ tool, onDecide }: { tool: ToolEntry; onDecide: (callId: string, approve: boolean) => void }) {
  const [busy, setBusy] = useState(false);
  const risk = riskTag[tool.risk];
  const st = statusText[tool.status];
  const decide = async (approve: boolean) => {
    setBusy(true);
    await onDecide(tool.call_id, approve);
    setBusy(false);
  };
  return (
    <div
      className={`flex flex-col gap-2 rounded-md border bg-surface-raised px-3 py-2 ${
        tool.status === "waiting" ? (tool.risk === "delete" ? "border-danger" : "border-warning") : "border-border"
      }`}
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="min-w-0 flex-1 text-[13px] font-medium text-ink">{tool.title}</span>
        <span className={`label ${risk.className}`}>{risk.label}</span>
        <span className={`label ${st.className}`}>{st.label}</span>
      </div>
      {tool.detail && (
        <pre className="code max-h-48 overflow-auto whitespace-pre-wrap rounded-sm bg-surface-sunken px-2.5 py-1.5 text-ink">{tool.detail}</pre>
      )}
      {tool.status === "waiting" && (
        <div className="flex gap-2">
          <Button variant={tool.risk === "delete" ? "danger" : "primary"} onClick={() => decide(true)} disabled={busy}>
            {tool.risk === "delete" ? "Approve delete" : "Approve"}
          </Button>
          <Button onClick={() => decide(false)} disabled={busy}>
            Refuse
          </Button>
        </div>
      )}
      {tool.output && tool.status !== "waiting" && (
        <details className="group">
          <summary className="cursor-pointer body-sm text-ink-muted hover:text-ink">{tool.status === "failed" ? "Error" : "Result"}</summary>
          <pre className="code mt-1.5 max-h-64 overflow-auto whitespace-pre-wrap rounded-sm bg-surface-sunken px-2.5 py-1.5 text-ink">{tool.output}</pre>
        </details>
      )}
    </div>
  );
}
