import type { FormEvent, ReactNode } from "react";
import { useAction } from "@/lib/use-action";
import { Button } from "./button";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "./dialog";
import { ErrorNotice } from "./feedback";
import { Fieldset } from "./fieldset";
// The confirm button's colour for each variant.
const confirmColors = { danger: "red", default: "dark/zinc" } as const;
type ConfirmationDialogProps = {
  open: boolean;
  onClose: () => void;
  title: string;
  description: string;
  // The confirm button's label, e.g. "Delete workspace".
  confirm: string;
  // "danger" for destructive actions: a red confirm button.
  variant?: keyof typeof confirmColors;
  // Keeps the confirm button off, e.g. until a name is typed.
  disabled?: boolean;
  onConfirm: () => Promise<unknown>;
  // Runs after the dialog has closed on success (a toast, navigation).
  onConfirmed?: () => unknown;
  // Handles a failure itself, as useAction's onError (an error on a field);
  // return true to keep it out of the dialog's error notice.
  onError?: (error: unknown) => boolean | void;
  // Extra fields (type-to-confirm, password), disabled while confirming.
  children?: ReactNode;
};
// Every "are you sure?": a title, what will happen, and Cancel or confirm.
// Confirming runs onConfirm through useAction: the button shows loading, a
// double click confirms once, and the dialog can't close until it settles.
// A failure keeps it open, with the error and the typed fields.
export function ConfirmationDialog({
  open,
  onClose,
  title,
  description,
  confirm,
  variant = "default",
  disabled = false,
  onConfirm,
  onConfirmed,
  onError,
  children,
}: ConfirmationDialogProps) {
  const action = useAction(onConfirm, { onError });
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (disabled || !(await action.run()).ok) return;
    onClose();
    await onConfirmed?.();
  }
  const close = () => !action.busy && onClose();
  return (
    <Dialog open={open} onClose={close} size="md">
      <form onSubmit={submit}>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
        <ErrorNotice error={action.error} className="mt-6" />
        {children && (
          <DialogBody>
            <Fieldset disabled={action.loading}>{children}</Fieldset>
          </DialogBody>
        )}
        <DialogActions>
          <Button plain onClick={close} disabled={action.loading}>
            Cancel
          </Button>
          <Button
            type="submit"
            color={confirmColors[variant]}
            loading={action.loading}
            disabled={disabled}
          >
            {confirm}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
