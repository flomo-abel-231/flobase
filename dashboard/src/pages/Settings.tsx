import type { ReactNode } from "react";
import type { ServerConfig } from "../api";
import { Card, PageHeader, ThemeSwitch } from "../ui";

export function Settings({ config }: { config: ServerConfig | null }) {
  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <PageHeader title="Settings" subtitle="This FloBase instance and how the dashboard looks to you." />

      <Card className="flex flex-col divide-y divide-border">
        <Row label="Mode" value={config ? (config.mode === "cloud" ? "Cloud: plans with prices, cloud regions" : "Self-hosted: one organisation, no billing") : "—"} />
        <Row label="Version" value={config ? <span className="font-mono">{config.version}</span> : "—"} />
        <Row label="Theme" value={<ThemeSwitch />} />
      </Card>

      <p className="body-sm text-ink-muted">
        Instance settings such as off-site storage, the gateway URL and the API token are set in <span className="font-mono">.env</span>{" "}
        and take effect when FloBase restarts.
      </p>
    </div>
  );
}

function Row({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
      <span className="label text-ink-muted">{label}</span>
      <span className="body-sm text-ink">{value}</span>
    </div>
  );
}
