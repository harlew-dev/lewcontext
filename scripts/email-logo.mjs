// Renders the app's mark (apps/web/public/favicon.svg) to the PNG the emails
// embed (apps/api/internal/mail/templates/logo.png): 64×64, shown at 32×32 so
// it's sharp on high-density screens, with a transparent background. Emails
// can't use the SVG itself: Gmail and Outlook don't display SVG.
// Run it after changing the logo: node scripts/email-logo.mjs
// (CHROMIUM_PATH as for the e2e tests.)
import { readFileSync } from "node:fs";
import { chromium } from "../apps/web/node_modules/playwright/index.mjs";

const root = new URL("..", import.meta.url);
const svg = readFileSync(new URL("apps/web/public/favicon.svg", root), "utf8");
const out = new URL("apps/api/internal/mail/templates/logo.png", root);

const browser = await chromium.launch({
  executablePath: process.env.CHROMIUM_PATH || "/usr/bin/chromium",
  args: ["--no-sandbox"],
});
const page = await browser.newPage({
  viewport: { width: 64, height: 64 },
  deviceScaleFactor: 1,
});
await page.setContent(
  `<style>html,body{margin:0;background:transparent}svg{display:block;width:64px;height:64px}</style>${svg}`,
);
await page.locator("svg").screenshot({ path: out.pathname, omitBackground: true });
await browser.close();
console.log(`wrote ${out.pathname}`);
