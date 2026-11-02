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
// The way back a step on the same sign-in page: a plain button with an
// arrow. It goes back to a step the browser's own Back can't reach (the code
// step to the password). Moving to another page is the browser's Back, or a
// link on the page. It usually sits left of the form's submit button, in
// that action row; a screen with no submit places it under the logo.
export function BackButton({
  onClick,
  disabled,
  className,
}: {
  onClick: () => void;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <Button plain onClick={onClick} disabled={disabled} className={className}>
      <ArrowLeftIcon />
      Back
    </Button>
  );
}
