import { useCallback, useEffect, useRef, useState } from "react";
import { CheckIcon, DocumentDuplicateIcon } from "@heroicons/react/16/solid";
import { popoverExit } from "@/lib/baseui";
import { Button } from "./button";
import { InputButton } from "./input";
import { Tooltip } from "./tooltip";
// How long the tooltip answers a click: "Copied" for 3 seconds, a failure
// (which says what to do instead) long enough to read.
const copiedFor = 3000;
const failedFor = 6000;
// Copies `value` and says so in a tooltip on the button that hides itself:
// "Copied" for 3 seconds, or `failed` (what to do instead, e.g. "Couldn’t
// copy. Select the ID and copy it instead.") where the clipboard isn't
// available. A screen reader hears the same from a status region. `onCopied`
// runs after each copy that worked.
//
// `variant`: "icon" is an InputButton for an InputGroup's InputActions,
// named `label` and with a "Copy" tooltip on hover; "button" is an outline
// Button reading `label` ("Copy codes").
export function CopyButton({
  value,
  label,
  failed = "Couldn’t copy. Select the text and copy it instead.",
  onCopied,
  variant = "icon",
}: {
  value: string;
  label: string;
  failed?: string;
  onCopied?: () => void;
  variant?: "icon" | "button";
}) {
  const { status, open, copied, copy } = useCopy(value, failed, onCopied);
  const icon = copied ? <CheckIcon /> : <DocumentDuplicateIcon />;
  return (
    <>
      <Tooltip
        content={status || "Copy"}
        open={open}
        // A labelled button needs no "Copy" on hover, only the answer.
        disabled={variant === "button" && !status}
      >
        {variant === "icon" ? (
          <InputButton aria-label={label} onClick={copy}>
            {icon}
          </InputButton>
        ) : (
          <Button outline onClick={copy}>
            {icon}
            {label}
          </Button>
        )}
      </Tooltip>
      <span role="status" className="sr-only">
        {status}
      </span>
    </>
  );
}
// The clipboard write and the tooltip's timing: `open` while the answer
// shows, `status` until it has faded out.
function useCopy(value: string, failed: string, onCopied?: () => void) {
  const [status, setStatus] = useState("");
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const timers = useRef<number[]>([]);
  const clear = useCallback(() => {
    timers.current.forEach((t) => window.clearTimeout(t));
    timers.current = [];
  }, []);
  useEffect(() => clear, [clear]);
  function answer(text: string, worked: boolean) {
    clear();
    const shown = worked ? copiedFor : failedFor;
    setStatus(text);
    setCopied(worked);
    setOpen(true);
    timers.current = [
      window.setTimeout(() => setOpen(false), shown),
      // Its text goes back to "Copy" once the tooltip has faded out.
      window.setTimeout(() => {
        setStatus("");
        setCopied(false);
      }, shown + popoverExit),
    ];
  }
  async function copy() {
    // Emptied first, so a second copy is announced again.
    clear();
    setStatus("");
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      answer(failed, false);
      return;
    }
    answer("Copied", true);
    onCopied?.();
  }
  return { status, open, copied: copied && open, copy };
}
