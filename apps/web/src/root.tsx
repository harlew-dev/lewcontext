import type { ReactNode } from "react";
import {
  isRouteErrorResponse,
  Links,
  Meta,
  Outlet,
  Scripts,
  ScrollRestoration,
} from "react-router";
import { BaseProvider } from "baseui";
import { ChallengeProvider } from "@/lib/challenge/challenge";
import { MotionConfig } from "motion/react";
import { SWRConfig } from "swr";
import { NotFound, ServiceUnavailable } from "@/components/layout/status-page";
import { RouteProgress } from "@/components/ui/route-progress";
import { appName } from "@/lib/app";
import { swrConfig } from "@/lib/swr";
import { theme } from "@/lib/theme";
import type { Route } from "./+types/root";
import "@/styles/globals.css";
export const meta: Route.MetaFunction = () => [
  { title: `${appName} — Context for AI agents` },
  {
    name: "description",
    content:
      "Context for AI agents, built from the documents you already have: a structured, project-specific wiki your agents can search and reason over.",
  },
];
export const links: Route.LinksFunction = () => [
  { rel: "icon", href: "/favicon.svg" },
];
// The HTML document around every page, including the prerendered ones.
export function Layout({ children }: { children: ReactNode }) {
  return (
    <html
      lang="en"
      className="text-zinc-950 antialiased lg:bg-zinc-100 dark:bg-zinc-900 dark:text-white dark:lg:bg-zinc-950"
    >
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <Meta />
        <Links />
      </head>
      <body>
        {children}
        <ScrollRestoration />
        <Scripts />
      </body>
    </html>
  );
}
// BaseUI's theme, SWR defaults and reduced-motion support for every page.
// Styletron's provider is in entry.client.tsx and entry.server.tsx.
function Providers({ children }: { children: ReactNode }) {
  return (
    <BaseProvider theme={theme}>
      <SWRConfig value={swrConfig}>
        <MotionConfig reducedMotion="user">
          <ChallengeProvider>{children}</ChallengeProvider>
        </MotionConfig>
      </SWRConfig>
    </BaseProvider>
  );
}
export default function App() {
  return (
    <Providers>
      <RouteProgress />
      <Outlet />
    </Providers>
  );
}
// Shown while the first page's route loader runs (the session check on
// signed-in pages). The SPA fallback HTML contains it.
export function HydrateFallback() {
  return <main aria-busy="true" className="min-h-dvh" />;
}
// A loader that throws a 404 renders the not-found page; anything else
// unexpected the outage page, which offers a reload.
export function ErrorBoundary({ error }: Route.ErrorBoundaryProps) {
  return (
    <Providers>
      {isRouteErrorResponse(error) && error.status === 404 ? (
        <NotFound />
      ) : (
        <ServiceUnavailable />
      )}
    </Providers>
  );
}
