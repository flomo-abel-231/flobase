import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Project } from "../../api";
import { Button, Notice } from "../../ui";

const serviceLabel: Record<string, string> = {
  db: "Database",
  pg: "Storage engine",
  pooler: "Pooler",
  rest: "REST API",
  auth: "Auth",
  offsite: "Off-site sync",
};

export function Logs({ project }: { project: Project }) {
  const [service, setService] = useState("db");
  const [services, setServices] = useState<string[]>([]);
  const [text, setText] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const box = useRef<HTMLPreElement>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.logs(project.ref, service);
      setServices(res.services);
      setText(res.text);
      setError("");
    } catch (e) {
      setText("");
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [project.ref, service]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [text]);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div role="group" aria-label="Service" className="inline-flex rounded-md border border-border-strong bg-surface-raised p-0.5">
          {services.map((s) => (
            <button
              key={s}
              type="button"
              aria-pressed={s === service}
              onClick={() => setService(s)}
              className={`rounded-sm px-3 py-1.5 text-[13px] font-medium ${
                s === service ? "bg-brand-subtle text-ink" : "text-ink-muted hover:text-ink"
              }`}
            >
              {serviceLabel[s] ?? s}
            </button>
          ))}
        </div>
        <Button onClick={load} disabled={loading}>
          {loading ? "Refreshing…" : "Refresh"}
        </Button>
      </div>
      {error && <Notice tone="warning">{error}</Notice>}
      <pre
        ref={box}
        className="code h-[60vh] overflow-auto whitespace-pre-wrap break-all rounded-md border border-border bg-surface-sunken p-4 text-ink"
      >
        {text || (loading ? "" : "No log lines yet.")}
      </pre>
      <p className="body-sm text-ink-muted">The last 300 lines from this service, newest at the bottom.</p>
    </div>
  );
}
