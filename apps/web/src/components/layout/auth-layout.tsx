import type { ReactNode } from "react";
import { PageTransition } from "./page-transition";
// AuthLayout: a centred card for sign-in and other standalone pages. Each
// page renders its own, so its content fades in as the page opens.
export function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <main className="flex min-h-dvh flex-col p-2">
      <div className="flex grow items-center justify-center p-6 lg:rounded-lg lg:bg-white lg:p-10 lg:shadow-xs lg:ring-1 lg:ring-zinc-950/5 dark:lg:bg-zinc-900 dark:lg:ring-white/10">
        <PageTransition className="flex w-full justify-center">
          {children}
        </PageTransition>
      </div>
    </main>
  );
}
