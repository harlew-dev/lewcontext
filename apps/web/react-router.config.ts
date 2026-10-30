import type { Config } from "@react-router/dev/config";
// A single-page app served by the Go API (apps/api/internal/web). Public pages
// listed in `prerender` are rendered to HTML at build time for search
// engines; every other path gets __spa-fallback.html and renders in the
// browser.
export default {
  appDirectory: "src",
  ssr: false,
  prerender: ["/"],
} satisfies Config;
