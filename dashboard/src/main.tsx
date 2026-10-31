import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import { App } from "./App";
import { setToken } from "./api";

// The flobase CLI and FloBase Desktop open the dashboard as
// http://localhost:8080/#token=<API token>. A URL fragment never reaches
// the server; the token is kept for this tab and removed from the address
// bar and history straight away.
if (window.location.hash.startsWith("#token=")) {
  setToken(decodeURIComponent(window.location.hash.slice("#token=".length)));
  window.history.replaceState(null, "", window.location.pathname + "#/");
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
