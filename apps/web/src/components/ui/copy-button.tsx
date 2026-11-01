import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { CheckIcon, DocumentDuplicateIcon } from "@heroicons/react/16/solid";
import { popoverExit } from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { Button } from "./button";
import { InputButton } from "./input";
import { Tooltip } from "./tooltip";
// How long "Copied" shows in the button's tooltip.
const copiedFor = 3000;
// A copy in progress or done: what useCopy returns, and what CopyButton and
// CopyFailure show.
export type Copy = {
  // Copies the value; resolves once the clipboard has answered.
  copy: () => Promise<void>;
  // "Copied" is showing (the check mark): 3 seconds, and its fade-out.
  copied: boolean;
  // The tooltip is held open to say "Copied": 3 seconds.
  open: boolean;
  // What the status region says ("Copied"), emptied before each try.
  announced: string;
  // The last try failed: CopyFailure says what to do instead until the
  // next one.
  failed: boolean;
};
// Copies `value` to the clipboard. After a copy that worked, `copied` is
// true for 3 seconds and `onCopied` runs; after one that failed, `failed`
// stays true until the next try. Nothing happens once the component has
// unmounted, however late the clipboard answers.
export function useCopy(
  value: string,
  { onCopied }: { onCopied?: () => void } = {},
): Copy {
  const [copied, setCopied] = useState(false);
  const [open, setOpen] = useState(false);
  const [failed, setFailed] = useState(false);
  const [announced, setAnnounced] = useState("");
  const timers = useRef<number[]>([]);
  const mounted = useRef(true);
  const clear = useCallback(() => {
    timers.current.forEach((t) => window.clearTimeout(t));
    timers.current = [];
  }, []);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      clear();
    };
  }, [clear]);
  async function copy() {
    clear();
    // Emptied first, so a second copy is announced again.
    setAnnounced("");
    setFailed(false);
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      if (mounted.current) setFailed(true);
      return;
    }
    if (!mounted.current) return;
    setAnnounced("Copied");
    setCopied(true);
    setOpen(true);
    timers.current = [
      window.setTimeout(() => setOpen(false), copiedFor),
      // The check mark goes once the tooltip has faded out.
      window.setTimeout(() => setCopied(false), copiedFor + popoverExit),
    ];
    onCopied?.();
  }
  return { copy, copied, open, announced, failed };
}
// A Copy button that answers in a tooltip on itself: "Copied" for 3
// seconds, then it hides. Screen readers hear "Copied" once, from a status
// region (the tooltip's text is hidden from them, so it isn't read again as
// the button's description). A failure is shown by CopyFailure, given the
// same `copy`, where the page has room for it.
//
// `variant`: "icon" is an InputButton for an InputGroup's InputActions,
// named `label` and with a "Copy" tooltip on hover; "button" is an outline
// Button reading `label` ("Copy codes").
export function CopyButton({
  copy,
  label,
  variant = "icon",
}: {
  copy: Copy;
  label: string;
  variant?: "icon" | "button";
}) {
  const icon = copy.copied ? <CheckIcon /> : <DocumentDuplicateIcon />;
  const run = () => void copy.copy();
  return (
    <>
      <Tooltip
        content={copy.copied ? <span aria-hidden="true">Copied</span> : "Copy"}
        open={copy.open}
        // A labelled button needs no "Copy" on hover, only the answer.
        disabled={variant === "button" && !copy.copied}
      >
        {variant === "icon" ? (
          <InputButton aria-label={label} onClick={run}>
            {icon}
          </InputButton>
        ) : (
          <Button outline onClick={run}>
            {icon}
            {label}
          </Button>
        )}
      </Tooltip>
      <span role="status" className="sr-only">
        {copy.announced}
      </span>
    </>
  );
}
// What to do instead when the clipboard isn't available ("Couldn’t copy.
// Select the ID and copy it instead."), shown under the field or button
// until the next try, and announced as an alert.
export function CopyFailure({
  copy,
  className,
  children,
}: {
  copy: Copy;
  className?: string;
  children: ReactNode;
}) {
  if (!copy.failed) return null;
  return (
    <p
      role="alert"
      className={cx(
        className,
        "text-base/6 text-zinc-500 sm:text-sm/6 dark:text-zinc-400",
      )}
    >
      {children}
    </p>
  );
}
