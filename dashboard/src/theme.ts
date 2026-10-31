// Theme preference: "system" follows the OS; "light" and "dark" set
// data-theme on <html>, which tokens.css reads. index.html applies the saved
// choice before first paint so the page never flashes the wrong theme.

export type ThemePref = "system" | "light" | "dark";

const KEY = "flobase.theme";

export function getThemePref(): ThemePref {
  try {
    const v = localStorage.getItem(KEY);
    return v === "light" || v === "dark" ? v : "system";
  } catch {
    return "system";
  }
}

export function setThemePref(pref: ThemePref) {
  try {
    if (pref === "system") localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, pref);
  } catch {
    // Storage can be blocked; the choice then lasts for this page only.
  }
  if (pref === "system") document.documentElement.removeAttribute("data-theme");
  else document.documentElement.setAttribute("data-theme", pref);
}
