import { useEffect, useState, type ReactNode } from "react";
import { api, formatMB, formatPrice, type Plan } from "../api";
import { Button, Card, Logo, ThemeSwitch } from "../ui";
import { navigate } from "../App";

// Home is the hosted cloud's front page for visitors who are not signed in:
// what FloBase is, what it costs, and the way in.

const features: { title: string; text: string; icon: ReactNode }[] = [
  {
    title: "Postgres, MySQL or MongoDB",
    text: "Pick an engine and get a running database with its connection string in about a minute.",
    icon: <path d="M4 6c0-1.7 2.7-3 6-3s6 1.3 6 3-2.7 3-6 3-6-1.3-6-3zm0 0v8c0 1.7 2.7 3 6 3s6-1.3 6-3V6M4 10c0 1.7 2.7 3 6 3s6-1.3 6-3" />,
  },
  {
    title: "Supabase-compatible APIs",
    text: "Postgres projects come with REST, GraphQL and Auth, so supabase-js works as it is.",
    icon: <path d="M7 6l-4 4 4 4M13 6l4 4-4 4M11 4l-2 12" />,
  },
  {
    title: "Table editor and SQL",
    text: "Browse every schema in a searchable, sortable grid, edit rows, and run SQL in the browser.",
    icon: <path d="M3 4h14v12H3zM3 8h14M3 12h14M8 4v12" />,
  },
  {
    title: "Backups you can restore",
    text: "Daily backups on paid plans, and point-in-time recovery on Pro and Business.",
    icon: <path d="M4 10a6 6 0 1 0 2-4.5M4 4v3h3M10 7v3l2 2" />,
  },
  {
    title: "Secure by default",
    text: "TLS on every database, per-project IP allow-lists, and workspaces that share nothing.",
    icon: <path d="M10 2l6 3v5c0 4-2.7 6.7-6 8-3.3-1.3-6-4-6-8V5z" />,
  },
  {
    title: "A built-in assistant",
    text: "Ask about your data in plain words; it runs the queries and shows the results.",
    icon: <path d="M4 5h12v8H9l-4 3v-3H4z" />,
  },
];

export function Home() {
  const [plans, setPlans] = useState<Plan[] | null>(null);

  useEffect(() => {
    api
      .publicPlans()
      .then((r) => setPlans(r.plans))
      .catch(() => setPlans([]));
  }, []);

  return (
    <div className="min-h-screen bg-surface">
      <header className="sticky top-0 z-10 border-b border-border bg-surface/85 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between gap-4 px-4 sm:px-6">
          <Logo />
          <nav className="flex items-center gap-2 sm:gap-4">
            <a href="#pricing" onClick={jump("pricing")} className="hidden text-[13px] text-ink-muted hover:text-ink sm:inline">
              Pricing
            </a>
            <ThemeSwitch />
            <Button onClick={() => navigate("/login")}>Sign in</Button>
            <Button variant="primary" onClick={() => navigate("/signup")}>
              Start free
            </Button>
          </nav>
        </div>
      </header>

      <main>
        <section className="mx-auto flex max-w-6xl flex-col items-center gap-6 px-4 pt-16 pb-14 text-center sm:px-6 sm:pt-24">
          <span className="rounded-full border border-border bg-surface-raised px-3 py-1 text-[12px] text-ink-muted">
            Open-source database platform
          </span>
          <h1 className="max-w-3xl text-4xl font-semibold tracking-tight text-ink sm:text-5xl">
            Your database, <span className="text-brand">ready in a minute</span>.
          </h1>
          <p className="max-w-2xl text-[17px] leading-7 text-ink-muted">
            FloBase runs Postgres, MySQL and MongoDB for you, with Supabase-compatible APIs, backups, a table editor and an AI
            assistant. Start free, no card needed.
          </p>
          <div className="flex flex-wrap justify-center gap-3">
            <Button variant="primary" className="h-10 px-5 text-[14px]" onClick={() => navigate("/signup")}>
              Create a free project
            </Button>
            <Button className="h-10 px-5 text-[14px]" onClick={jump("pricing")}>
              See plans
            </Button>
          </div>
        </section>

        <section className="mx-auto max-w-6xl px-4 pb-20 sm:px-6">
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {features.map((f) => (
              <Card key={f.title} className="flex flex-col gap-2 p-5">
                <span className="flex size-9 items-center justify-center rounded-lg bg-brand-subtle text-brand">
                  <svg viewBox="0 0 20 20" className="size-5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                    {f.icon}
                  </svg>
                </span>
                <h3 className="font-medium text-ink">{f.title}</h3>
                <p className="body-sm text-ink-muted">{f.text}</p>
              </Card>
            ))}
          </div>
        </section>

        <section id="pricing" className="border-y border-border bg-surface-sunken/60">
          <div className="mx-auto flex max-w-6xl flex-col gap-8 px-4 py-20 sm:px-6">
            <div className="flex flex-col items-center gap-2 text-center">
              <h2 className="text-3xl font-semibold tracking-tight text-ink">Simple monthly plans</h2>
              <p className="max-w-xl text-ink-muted">Start on Free to try FloBase. Move a project to a bigger plan whenever you need to.</p>
            </div>
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
              {(plans ?? []).map((p) => (
                <PlanCard key={p.id} plan={p} featured={p.id === "pro"} />
              ))}
            </div>
          </div>
        </section>

        <section className="mx-auto flex max-w-6xl flex-col items-center gap-5 px-4 py-20 text-center sm:px-6">
          <h2 className="text-3xl font-semibold tracking-tight text-ink">Build something today</h2>
          <p className="max-w-xl text-ink-muted">Create an account and your first database in under a minute.</p>
          <Button variant="primary" className="h-10 px-5 text-[14px]" onClick={() => navigate("/signup")}>
            Start free
          </Button>
        </section>
      </main>

      <footer className="border-t border-border">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3 px-4 py-6 body-sm text-ink-muted sm:px-6">
          <Logo />
          <span>© {new Date().getFullYear()} Manevia LLC</span>
        </div>
      </footer>
    </div>
  );
}

function PlanCard({ plan, featured }: { plan: Plan; featured: boolean }) {
  const lines = [
    `${plan.cpu} vCPU · ${plan.memory_mb >= 1024 ? `${plan.memory_mb / 1024} GB` : `${plan.memory_mb} MB`} RAM`,
    `${formatMB(Math.round(plan.disk_gb * 1000))} database`,
    `${plan.files_gb} GB file storage`,
    `${plan.egress_gb} GB transfer`,
    plan.backups === "None" ? "No backups" : `${plan.backups} backups`,
  ];
  if (plan.deleted_after_days) lines.push(`Deleted after ${plan.deleted_after_days} days`);
  return (
    <Card className={`flex flex-col gap-4 p-5 ${featured ? "border-brand ring-1 ring-brand" : ""}`}>
      <div className="flex items-center justify-between">
        <h3 className="font-medium text-ink">{plan.name}</h3>
        {featured && <span className="rounded-full bg-brand-subtle px-2 py-0.5 text-[11px] font-medium text-brand">Popular</span>}
      </div>
      <div>
        <span className="text-3xl font-semibold text-ink">{formatPrice(plan.price_usd)}</span>
        <span className="body-sm text-ink-muted"> /month</span>
      </div>
      <p className="body-sm text-ink-muted">{plan.description}</p>
      <ul className="flex flex-col gap-1.5 body-sm text-ink">
        {lines.map((l) => (
          <li key={l} className="flex gap-2">
            <span className="text-brand">✓</span>
            {l}
          </li>
        ))}
      </ul>
      <div className="mt-auto pt-2">
        {plan.available ? (
          <Button variant={featured ? "primary" : "secondary"} className="w-full" onClick={() => navigate("/signup")}>
            {plan.price_usd === 0 ? "Start free" : `Choose ${plan.name}`}
          </Button>
        ) : (
          <Button className="w-full" disabled>
            Coming soon
          </Button>
        )}
      </div>
    </Card>
  );
}

// jump scrolls to a section without touching the hash, which holds the
// dashboard's route.
function jump(id: string) {
  return (e: { preventDefault: () => void }) => {
    e.preventDefault();
    document.getElementById(id)?.scrollIntoView({ behavior: "smooth" });
  };
}
