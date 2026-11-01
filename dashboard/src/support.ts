// How people can support FloBase. FloBase is free, open-source software
// (Apache 2.0); donations, grants and paid support keep it going.
//
// To take donations, create Payment Links in your Stripe dashboard
// (Payment Links → New → "Customers choose what to pay", one one-time and
// one recurring monthly) and paste their URLs below. Empty links show as
// "coming soon". Payments go straight to Stripe: FloBase never sees them.
export const support = {
  stripeOneTime: "",
  stripeMonthly: "",
  // Where organizations write about grants, sponsorship and paid support.
  // Empty = a GitHub issue on the repository.
  contactEmail: "",
  repository: "https://github.com/flomo-abel-231/flobase",
};

export function contactLink(subject: string): string {
  if (support.contactEmail) return `mailto:${support.contactEmail}?subject=${encodeURIComponent(subject)}`;
  return `${support.repository}/issues/new?title=${encodeURIComponent(subject)}`;
}
