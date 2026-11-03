import { useEffect, useRef, useState, type ReactNode } from "react";
import { EyeIcon, EyeSlashIcon } from "@heroicons/react/16/solid";
import { Button } from "./button";
// Something secret that stays off the screen until asked for, in case it's
// shared, recorded or overlooked: a QR code for an authenticator app,
// recovery codes. Until revealed, `children` aren't rendered at all (not
// blurred, which a screenshot could undo); a blurred `placeholder` stands in,
// with the reveal button over it. Revealing moves focus to the content and
// says so; Hide puts the placeholder back. Key it to start hidden again.
export function Reveal({
  label,
  hideLabel,
  announcement,
  placeholder,
  children,
  onReveal,
  className,
}: {
  // The button's text, e.g. "Reveal QR code", and Hide's, "Hide QR code".
  label: string;
  hideLabel: string;
  // What screen readers hear once revealed, e.g. "QR code shown."
  announcement: string;
  placeholder: ReactNode;
  children: ReactNode;
  onReveal?: () => void;
  className?: string;
}) {
  const [revealed, setRevealed] = useState(false);
  const content = useRef<HTMLDivElement>(null);
  const reveal = useRef<HTMLElement>(null);
  // null until the visitor first acts, so mounting doesn't move focus.
  const [moved, setMoved] = useState<"revealed" | "hidden" | null>(null);
  useEffect(() => {
    if (moved === "revealed") content.current?.focus();
    if (moved === "hidden") reveal.current?.focus();
  }, [moved]);
  return (
    <div className={className}>
      {revealed ? (
        <div className="space-y-3">
          <div
            ref={content}
            tabIndex={-1}
            className="rounded-lg outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500"
          >
            {children}
          </div>
          <div className="flex justify-center">
            <Button
              plain
              onClick={() => {
                setRevealed(false);
                setMoved("hidden");
              }}
            >
              <EyeSlashIcon />
              {hideLabel}
            </Button>
          </div>
        </div>
      ) : (
        <div className="relative isolate">
          <div
            aria-hidden="true"
            className="pointer-events-none blur-md select-none"
          >
            {placeholder}
          </div>
          <div className="absolute inset-0 flex items-center justify-center">
            {/* A solid surface, so the button reads over the blur. */}
            <div className="rounded-lg bg-white shadow-sm dark:bg-zinc-900">
              <Button
                outline
                ref={reveal}
                onClick={() => {
                  setRevealed(true);
                  setMoved("revealed");
                  onReveal?.();
                }}
              >
                <EyeIcon />
                {label}
              </Button>
            </div>
          </div>
        </div>
      )}
      {/* Present from the start, so the change is announced. */}
      <p role="status" className="sr-only">
        {revealed ? announcement : ""}
      </p>
    </div>
  );
}
