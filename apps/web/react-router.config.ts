import type { Config } from "@react-router/dev/config";
import { webBuild } from "./web-build.ts";
// A single-page app served by the Go API (apps/api/internal/web), built twice
// (web-build.ts). The app build prerenders the public pages listed in
// `prerender` for search engines (the policy pages' loaders read their
// Markdown then, so it never reaches the browser; Go serves those two only
// with POLICIES=on); every other path gets __spa-fallback.html
// and renders in the browser. The admin build prerenders nothing: its one
// HTML file is index.html, which Go serves only at the right key.
const build = webBuild();

export default {
  appDirectory: "src",
  ssr: false,
  buildDirectory: build === "admin" ? "build/admin" : "build",
  prerender:
    build === "admin"
      ? false
      : ["/", "/policy/terms-of-service", "/policy/privacy-policy"],
} satisfies Config;
