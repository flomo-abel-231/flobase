// FloBase design tokens as typed values, for charts, canvas and anything
// that cannot read CSS variables. In components, prefer the CSS variables.

export const themes = {
  light: {
    surface: "#faf7f4",
    surfaceRaised: "#ffffff",
    surfaceSunken: "#f2ede8",
    border: "#e2dad2",
    borderStrong: "#80746a",
    ink: "#1c1612",
    inkMuted: "#5e544c",
    brand: "#b0581f",
    onBrand: "#ffffff",
    brandSubtle: "#f7e7d9",
    accent: "#0f6e6e",
    success: "#1d7a4a",
    warning: "#7a5a00",
    danger: "#b42318",
  },
  dark: {
    surface: "#141110",
    surfaceRaised: "#1d1916",
    surfaceSunken: "#0e0c0b",
    border: "#332c27",
    borderStrong: "#7d7168",
    ink: "#f3eee9",
    inkMuted: "#a89c92",
    brand: "#f4ad6c",
    onBrand: "#1f1208",
    brandSubtle: "#3e2717",
    accent: "#4cc9c0",
    success: "#5bd496",
    warning: "#f0c054",
    danger: "#ff8a7a",
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
