// How people can support FloBase. FloBase is free, open-source software
// (Apache 2.0); donations, grants and paid support keep it going.
//
// One-time donations are Stripe Payment Links on Manevia LLC's account, one
// per amount, all for the "Donation to FloBase" product. Payments go
// straight to Stripe: FloBase never sees them. An empty list hides them.
export const support = {
  stripeDonations: [
    { amount: 1, url: "https://donate.stripe.com/dRmaEW7UzaIc5EMeRM4Ja00" },
    { amount: 2, url: "https://donate.stripe.com/dRmbJ0deTcQk2sAcJE4Ja01" },
    { amount: 5, url: "https://donate.stripe.com/eVqcN42AfbMg1ow4d84Ja02" },
    { amount: 10, url: "https://donate.stripe.com/cNicN42Af03y0ks2504Ja03" },
    { amount: 20, url: "https://donate.stripe.com/28E6oG2Af7w0aZ6dNI4Ja04" },
    { amount: 50, url: "https://donate.stripe.com/eVqdR88YD9E85EMfVQ4Ja05" },
    { amount: 100, url: "https://donate.stripe.com/28EcN4deTcQkc3a2504Ja06" },
    { amount: 500, url: "https://donate.stripe.com/4gMcN41wbeYsffmeRM4Ja07" },
  ],
  // GitHub account whose Sponsors page takes sponsorships
  // (github.com/sponsors/<name>); empty hides the button. GitHub Sponsors
  // has monthly and one-time tiers and takes no fee on personal accounts.
  githubSponsors: "flomo-abel-231",
  // Where organizations write about grants, sponsorship and paid support.
  // Empty = a GitHub issue on the repository.
  contactEmail: "",
  repository: "https://github.com/flomo-abel-231/flobase",
};

export function contactLink(subject: string): string {
  if (support.contactEmail) return `mailto:${support.contactEmail}?subject=${encodeURIComponent(subject)}`;
  return `${support.repository}/issues/new?title=${encodeURIComponent(subject)}`;
}
