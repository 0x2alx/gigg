import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./theme.css";
import { App } from "./App";
import { TooltipLayer } from "./tooltip";
import { applyTheme, initialTheme } from "./theme";

// Apply the cached theme before the first render (the user's saved choice replaces it after login).
applyTheme(initialTheme());

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
    <TooltipLayer />
  </StrictMode>,
);
