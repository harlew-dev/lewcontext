import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Notice } from "@/components/ui/feedback";
import { Text } from "@/components/ui/text";
import type { Challenge } from "@/lib/api/http";
import { loadScript, providers } from "@/lib/challenge/providers";
// "Quick security check": a rate limit's challenge, shown by
// ChallengeProvider while a request waits for its token. The provider's
// widget renders inside, usually with nothing to click (it checks and the
// dialog closes by itself); when the provider wants an interaction, it shows
// here. Cancel rejects the request as "Check cancelled.". Shared by login,
// register, forgot password and members (whatever request is challenged).

type State =
  | { kind: "loading" }
  | { kind: "checking" }
  | { kind: "done" }
  | { kind: "failed"; message: string; retry: boolean };

const unsupported =
  "This check isn’t supported. Refresh the page and try again.";
const blocked =
  "Couldn’t load the check. Check your connection or turn off blockers for this site, then try again.";

export function ChallengeDialog({
  challenge,
  onSolved,
  onCancel,
}: {
  challenge: Challenge | null;
  onSolved: (token: string) => void;
  onCancel: () => void;
}) {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [attempt, setAttempt] = useState(0);
  const slot = useRef<HTMLDivElement>(null);
  // Own keys only, so a provider named "constructor" is unsupported too.
  const widget =
    challenge && Object.hasOwn(providers, challenge.provider)
      ? providers[challenge.provider]
      : undefined;
  useEffect(() => {
    if (!challenge || !widget) return;
    let live = true;
    let remove: (() => void) | undefined;
    loadScript(widget.script).then(
      () => {
        if (!live || !slot.current) return;
        setState({ kind: "checking" });
        remove = widget.render(slot.current, {
          siteKey: challenge.siteKey,
          action: challenge.action,
          theme: "auto",
          onToken: (token) => {
            if (!live) return;
            setState({ kind: "done" });
            onSolved(token);
          },
          onError: (message) =>
            live && setState({ kind: "failed", message, retry: true }),
          // An unused token expired: the widget resets itself.
          onExpire: () => live && setState({ kind: "checking" }),
        });
      },
      () => live && setState({ kind: "failed", message: blocked, retry: true }),
    );
    return () => {
      live = false;
      remove?.();
    };
  }, [challenge, widget, attempt, onSolved]);
  // A provider this page has no adapter for (a newer server).
  const shown: State =
    challenge && !widget
      ? { kind: "failed", message: unsupported, retry: false }
      : state;
  const status =
    shown.kind === "done" ? "Done" : shown.kind === "failed" ? "" : "Checking…";
  return (
    <Dialog open={challenge !== null} onClose={onCancel} size="sm">
      <DialogTitle>Quick security check</DialogTitle>
      <DialogDescription>
        We’re seeing a lot of traffic. Complete this check to continue.
      </DialogDescription>
      <DialogBody className="space-y-4">
        {/* The provider's widget, when it shows one. */}
        <div
          ref={slot}
          data-challenge-widget=""
          className="flex justify-center empty:hidden"
        />
        <Text role="status">{status}</Text>
        {shown.kind === "failed" && (
          <Notice
            variant="error"
            onRetry={
              shown.retry
                ? () => {
                    setState({ kind: "loading" });
                    setAttempt((n) => n + 1);
                  }
                : undefined
            }
          >
            {shown.message}
          </Notice>
        )}
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onCancel}>
          Cancel
        </Button>
      </DialogActions>
    </Dialog>
  );
}
