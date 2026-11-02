import type { ReactNode } from "react";
import { ArrowLeftIcon } from "@heroicons/react/16/solid";
import { Button } from "@/components/ui/button";
import { PageTransition } from "./page-transition";
// AuthLayout: a centred card for sign-in and other standalone pages. Each
// page renders its own, so its content fades in as the page opens.
// `footer` goes below the card (the policy links).
export function AuthLayout({
  children,
  footer,
}: {
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <main className="flex min-h-dvh flex-col p-2">
      <div className="flex grow items-center justify-center p-6 lg:rounded-lg lg:bg-white lg:p-10 lg:shadow-xs lg:ring-1 lg:ring-zinc-950/5 dark:lg:bg-zinc-900 dark:lg:ring-white/10">
        <PageTransition className="flex w-full justify-center">
          {children}
        </PageTransition>
      </div>
      {footer && (
        <footer className="flex justify-center px-6 pt-4 pb-2">{footer}</footer>
      )}
    </main>
  );
}
// The way back from a sign-in page, right under its logo: a plain button
// with an arrow, its arrow in line with the logo. With `href` it opens the
// page before (with navigation `state`, e.g. the typed email); with
// `onClick` it goes back a step on the same page (the code step to the
// password). It replaces the "back to …" links at the foot of the form.
export function BackButton(
  props:
    | { href: string; state?: unknown }
    | { onClick: () => void; disabled?: boolean },
) {
  const className = "-mt-4 -ml-3 justify-self-start sm:-ml-2.5";
  const content = (
    <>
      <ArrowLeftIcon />
      Back
    </>
  );
  if ("href" in props)
    return (
      <Button plain href={props.href} state={props.state} className={className}>
        {content}
      </Button>
    );
  return (
    <Button
      plain
      onClick={props.onClick}
      disabled={props.disabled}
      className={className}
    >
      {content}
    </Button>
  );
}
