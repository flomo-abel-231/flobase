// FloBase design tokens as typed values, for charts, canvas and anything
// that cannot read CSS variables. In components, prefer the CSS variables.

export const themes = {
  light: {
    surface: "#f6f9fc",
    surfaceRaised: "#ffffff",
    surfaceSunken: "#edf2f7",
    border: "#d8e1ea",
    borderStrong: "#687889",
    ink: "#0f172a",
    inkMuted: "#475569",
    brand: "#0270b0",
    onBrand: "#ffffff",
    brandSubtle: "#e0f2fe",
    accent: "#6d28d9",
    success: "#15803d",
    warning: "#a16207",
    danger: "#b91c1c",
  },
  dark: {
    surface: "#0b1220",
    surfaceRaised: "#111a2b",
    surfaceSunken: "#070c16",
    border: "#1f2a3c",
    borderStrong: "#64748b",
    ink: "#e8eef6",
    inkMuted: "#94a3b8",
    brand: "#38bdf8",
    onBrand: "#062538",
    brandSubtle: "#0c3550",
    accent: "#c4b5fd",
    success: "#4ade80",
    warning: "#facc15",
    danger: "#f87171",
  },
} as const;

export type ThemeName = keyof typeof themes;
export type ColorToken = keyof (typeof themes)["light"];

export const space = { 1: 4, 2: 8, 3: 12, 4: 16, 6: 24, 8: 32, 12: 48 } as const;
export const radius = { sm: 4, md: 8, lg: 12, full: 9999 } as const;

export const fonts = {
  sans: '"Geist", system-ui, sans-serif',
  mono: '"Geist Mono", ui-monospace, monospace',
} as const;

export const typeScale = {
  display: { size: 48, lineHeight: 52, weight: 600, family: "sans" },
  h1: { size: 32, lineHeight: 38, weight: 600, family: "sans" },
  h2: { size: 24, lineHeight: 30, weight: 600, family: "sans" },
  h3: { size: 18, lineHeight: 24, weight: 600, family: "sans" },
  body: { size: 15, lineHeight: 22, weight: 400, family: "sans" },
  bodySm: { size: 13, lineHeight: 18, weight: 400, family: "sans" },
  label: { size: 12, lineHeight: 16, weight: 500, family: "sans" },
  code: { size: 13, lineHeight: 20, weight: 400, family: "mono" },
  metric: { size: 28, lineHeight: 32, weight: 500, family: "mono" },
} as const;
