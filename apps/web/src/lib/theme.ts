import { createTheme } from "baseui";
// BaseUI's own styling is mostly replaced by Tailwind override slots; these
// primitives cover anything it still draws itself (font, focus, fallbacks).
export const theme = createTheme({
  primaryFontFamily: '"Inter Variable", ui-sans-serif, system-ui, sans-serif',
  primary: "#18181b",
  accent: "#3b82f6",
});
