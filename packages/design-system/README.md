# FloBase design system (code)

Code version of the FloBase design system: the same colours, type, spacing and radii as the design-system page.

## Files

- `tokens.css`: CSS variables for light and dark themes, spacing, radii, fonts, focus ring and type classes (`.h1`, `.body`, `.code`, …). Works with any framework.
- `tailwind.css`: Tailwind CSS v4 theme built on `tokens.css` (`bg-surface-raised`, `text-ink-muted`, `bg-brand text-on-brand`, `rounded-lg`).
- `tokens.ts`: the same values as typed constants, for charts and canvas.

## Use

Plain CSS: import `tokens.css` once at the app root.

Tailwind v4: in your main CSS file, replace `@import "tailwindcss";` with `@import "@flobase/design-system/tailwind.css";`.

Theme: dark follows the OS by default. Force one with `<html data-theme="light">` or `<html data-theme="dark">`.

## Rules

- `brand` is the only interactive colour; its fill always takes `on-brand` text. One primary button per view.
- Cards sit on `surface-raised` with a 1px `border`; no shadows except `shadow-overlay` on menus, popovers and dialogs.
- Status colours (`success`, `warning`, `danger`) always come with a word or icon.
- Control edges use `border-strong`; `border` is for dividers only.
- Anything a developer copies (SQL, keys, connection strings) is set in the mono font.
