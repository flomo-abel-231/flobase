// Dashboard building blocks on the FloBase design system, in the style of
// Coolify's current dashboard: compact 32px controls with 8px corners and a
// small depth shadow, rounded cards that lift on hover, hairline borders.
// Rules they follow: brand is the only interactive colour, status always
// carries a word, and anything copyable is set in mono.

import { useState, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode } from "react";
import type { Status } from "./api";
import { getThemePref, setThemePref, type ThemePref } from "./theme";

type ButtonVariant = "primary" | "secondary" | "danger";

const buttonStyles: Record<ButtonVariant, string> = {
  primary: "bg-brand text-on-brand border border-brand hover:brightness-110",
  secondary: "bg-surface-raised text-ink border border-border hover:border-border-strong",
  danger: "bg-surface-raised text-danger border border-danger/60 hover:border-danger",
};

export function Button({
  variant = "secondary",
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant }) {
  return (
    <button
      className={`inline-flex h-8 items-center justify-center gap-1.5 rounded-lg px-3 text-[13px] font-medium shadow-[0_2px_0_rgb(0_0_0/0.22)] transition duration-[120ms] hover:-translate-y-px active:translate-y-0.5 active:shadow-none disabled:translate-y-0 disabled:cursor-not-allowed disabled:opacity-50 disabled:shadow-none ${buttonStyles[variant]} ${className}`}
      {...props}
    />
  );
}

export function Card({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`rounded-xl border border-border bg-surface-raised shadow-sm ${className}`}>{children}</div>;
}

// Box is a clickable resource card, as Coolify lists projects: an icon
// tile, the title with a line of detail, and a footer for status.
export function Box({
  href,
  title,
  detail,
  aside,
  icon,
  children,
}: {
  href: string;
  title: ReactNode;
  detail?: ReactNode;
  aside?: ReactNode;
  icon?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <a
      href={href}
      className="group flex min-h-28 flex-col rounded-xl border border-border bg-surface-raised p-3 shadow-sm transition duration-150 hover:-translate-y-px hover:border-border-strong hover:shadow-md"
    >
      <div className="flex min-w-0 items-start gap-2.5">
        {icon && (
          <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-border bg-surface text-[12px] font-semibold text-ink-muted">
            {icon}
          </span>
        )}
        <span className="flex min-w-0 flex-col">
          <span className="truncate text-[13px] leading-4 font-semibold text-ink">{title}</span>
          {detail && <span className="mt-0.5 truncate text-[11px] leading-4 text-ink-muted">{detail}</span>}
        </span>
      </div>
      {children}
      {aside && <div className="mt-auto flex items-center justify-between border-t border-border pt-2.5">{aside}</div>}
    </a>
  );
}

// PageHeader puts the page's title on the left and its one action on the
// right, Coolify style.
export function PageHeader({ title, action, subtitle }: { title: ReactNode; action?: ReactNode; subtitle?: ReactNode }) {
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="truncate text-[24px] leading-7 font-semibold tracking-tight text-ink">{title}</h1>
        {subtitle && <p className="body-sm max-w-3xl text-ink-muted">{subtitle}</p>}
      </div>
      {action && <div className="flex shrink-0 items-center gap-2">{action}</div>}
    </div>
  );
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className="text-[13px] leading-4 font-medium text-ink">{label}</span>
      {children}
      {hint && <span className="body-sm text-ink-muted">{hint}</span>}
    </label>
  );
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className="h-8 rounded-lg border border-border bg-surface-sunken px-2.5 text-[14px] text-ink transition duration-[120ms] placeholder:text-ink-muted hover:border-border-strong focus:border-brand focus:outline-none max-sm:text-[16px]"
      {...props}
    />
  );
}

const statusStyles: Record<Status, { dot: string; text: string; label: string }> = {
  active: { dot: "bg-success", text: "text-success", label: "Active" },
  provisioning: { dot: "bg-warning animate-pulse", text: "text-warning", label: "Provisioning" },
  deleting: { dot: "bg-warning animate-pulse", text: "text-warning", label: "Deleting" },
  failed: { dot: "bg-danger", text: "text-danger", label: "Failed" },
};

export function StatusBadge({ status }: { status: Status }) {
  const s = statusStyles[status];
  return (
    <span className={`inline-flex items-center gap-2 label ${s.text}`}>
      <span className={`size-2 rounded-full ${s.dot}`} aria-hidden />
      {s.label}
    </span>
  );
}

export function Tag({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex items-center rounded-md border border-border bg-surface px-1.5 py-0.5 label text-ink-muted">
      {children}
    </span>
  );
}

export function CopyField({ label, value, secret = false }: { label: string; value: string; secret?: boolean }) {
  const [copied, setCopied] = useState(false);
  const [shown, setShown] = useState(!secret);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard can be blocked; the value stays selectable.
    }
  };
  return (
    <div className="flex flex-col gap-2">
      <span className="label text-ink">{label}</span>
      <div className="flex min-w-0 items-center gap-2">
        <code className="code min-w-0 flex-1 truncate rounded-lg border border-border bg-surface-sunken px-2.5 py-1.5 text-ink">
          {shown ? value : "•".repeat(Math.min(value.length, 32))}
        </code>
        {secret && (
          <Button onClick={() => setShown(!shown)} aria-label={shown ? `Hide ${label}` : `Show ${label}`}>
            {shown ? "Hide" : "Show"}
          </Button>
        )}
        <Button onClick={copy} aria-label={`Copy ${label}`}>
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
    </div>
  );
}

export function Notice({ tone, children }: { tone: "danger" | "warning"; children: ReactNode }) {
  const color = tone === "danger" ? "border-danger text-danger" : "border-warning text-warning";
  return (
    <div role="alert" className={`rounded-xl border bg-surface-raised px-4 py-3 body-sm ${color}`}>
      <strong className="font-medium">{tone === "danger" ? "Error: " : "Note: "}</strong>
      {children}
    </div>
  );
}

const themeOptions: { value: ThemePref; label: string; icon: ReactNode }[] = [
  {
    value: "system",
    label: "System theme",
    icon: <path d="M3 4h14v9H3zM7 16h6M10 13v3" />,
  },
  {
    value: "light",
    label: "Light theme",
    icon: (
      <>
        <circle cx="10" cy="10" r="3.5" />
        <path d="M10 2v2M10 16v2M2 10h2M16 10h2M4.3 4.3l1.4 1.4M14.3 14.3l1.4 1.4M4.3 15.7l1.4-1.4M14.3 5.7l1.4-1.4" />
      </>
    ),
  },
  {
    value: "dark",
    label: "Dark theme",
    icon: <path d="M16 12.5A6.5 6.5 0 0 1 7.5 4a6.5 6.5 0 1 0 8.5 8.5z" />,
  },
];

export function ThemeSwitch() {
  const [pref, setPref] = useState<ThemePref>(getThemePref);
  const choose = (value: ThemePref) => {
    setThemePref(value);
    setPref(value);
  };
  return (
    <div role="group" aria-label="Theme" className="inline-flex rounded-lg border border-border bg-surface-raised p-0.5">
      {themeOptions.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-label={o.label}
          aria-pressed={pref === o.value}
          title={o.label}
          onClick={() => choose(o.value)}
          className={`inline-flex size-7 items-center justify-center rounded-md transition ${
            pref === o.value ? "bg-brand-subtle text-brand" : "text-ink-muted hover:text-ink"
          }`}
        >
          <svg viewBox="0 0 20 20" className="size-4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
            {o.icon}
          </svg>
        </button>
      ))}
    </div>
  );
}

export function Logo({ version }: { version?: string }) {
  return (
    <span className="inline-flex items-center gap-2">
      <svg viewBox="0 0 32 32" className="size-6" aria-hidden>
        <rect width="32" height="32" rx="8" className="fill-brand" />
        <path d="M10 9h13v3.5h-9v3h7.5V19H14v5h-4z" className="fill-on-brand" />
      </svg>
      <span className="text-[15px] font-semibold tracking-tight text-ink">FloBase</span>
      {version && <span className="rounded-md border border-border px-1 text-[10.5px] leading-4 text-ink-muted">v{version}</span>}
    </span>
  );
}
