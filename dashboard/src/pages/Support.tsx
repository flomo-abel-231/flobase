import type { ReactNode } from "react";
import { Card, PageHeader } from "../ui";
import { contactLink, support } from "../support";

// Support FloBase: donations through Stripe, grants and sponsorship, paid
// support, and the free ways to help. The page only links out; no payment
// details pass through FloBase.

function Way({ title, children, action }: { title: string; children: ReactNode; action: ReactNode }) {
  return (
    <Card className="flex flex-col gap-3 p-5">
      <h2 className="text-[15px] font-semibold text-ink">{title}</h2>
      <div className="body-sm flex-1 text-ink-muted">{children}</div>
      <div className="flex flex-wrap gap-2">{action}</div>
    </Card>
  );
}

// LinkButton looks like the dashboard's buttons but opens a page.
function LinkButton({ href, primary, children }: { href: string; primary?: boolean; children: ReactNode }) {
  const style = primary
    ? "bg-brand text-on-brand border-brand hover:brightness-110"
    : "bg-surface-raised text-ink border-border hover:border-border-strong";
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className={`inline-flex h-8 items-center justify-center gap-1.5 rounded-lg border px-3 text-[13px] font-medium shadow-[0_2px_0_rgb(0_0_0/0.22)] transition duration-[120ms] hover:-translate-y-px active:translate-y-0.5 active:shadow-none ${style}`}
    >
      {children}
    </a>
  );
}

function Soon({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex h-8 items-center rounded-lg border border-dashed border-border px-3 text-[13px] text-ink-muted">
      {children} · coming soon
    </span>
  );
}

export function Support() {
  return (
    <div className="flex max-w-5xl flex-col gap-6">
      <PageHeader
        title="Support FloBase"
        subtitle="FloBase is free, open-source software you host yourself, under the Apache 2.0 licence. There are no paid plans: donations, grants and support contracts pay for its development."
      />

      <Card className="flex flex-col gap-4 border-brand/40 bg-brand-subtle/40 p-6 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-col gap-1">
          <h2 className="text-[18px] font-semibold tracking-tight text-ink">Donate</h2>
          <p className="body-sm max-w-xl text-ink-muted">
            Give once or every month, any amount. Payments are handled by Stripe; you choose the amount on Stripe's page and get a
            receipt by email.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {support.stripeMonthly ? (
            <LinkButton href={support.stripeMonthly} primary>
              Give monthly
            </LinkButton>
          ) : (
            <Soon>Give monthly</Soon>
          )}
          {support.stripeOneTime ? (
            <LinkButton href={support.stripeOneTime} primary={!support.stripeMonthly}>
              Give once
            </LinkButton>
          ) : (
            <Soon>Give once</Soon>
          )}
        </div>
      </Card>

      <div className="grid gap-3 md:grid-cols-3">
        <Way
          title="Grants and sponsorship"
          action={<LinkButton href={contactLink("Sponsoring FloBase")}>Get in touch</LinkButton>}
        >
          Foundations, public programmes and companies can fund FloBase's roadmap: new engines, region servers, security reviews.
          Sponsors are thanked in the README and release notes.
        </Way>
        <Way
          title="Paid support"
          action={<LinkButton href={contactLink("Support for FloBase")}>Ask about support</LinkButton>}
        >
          Help setting up FloBase on your servers, moving databases in, and a direct line when something breaks. Every contract
          also funds the open-source work.
        </Way>
        <Way
          title="Help for free"
          action={
            <>
              <LinkButton href={support.repository}>Star on GitHub</LinkButton>
              <LinkButton href={`${support.repository}/issues/new`}>Report a bug</LinkButton>
            </>
          }
        >
          Star the repository, report bugs, improve the docs or send a pull request. Telling others you use FloBase helps too.
        </Way>
      </div>
    </div>
  );
}
