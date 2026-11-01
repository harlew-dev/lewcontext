import { useState } from "react";
import { AppOrRecoveryCode } from "@/components/app/mfa/mfa-setup";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
import {
  Description,
  ErrorMessage,
  Field,
  FieldGroup,
  Label,
} from "@/components/ui/fieldset";
import { Textarea } from "@/components/ui/textarea";
import { Text } from "@/components/ui/text";
import { ApiError, errorMessage } from "@/lib/api";
import { useNavigate } from "react-router";
import { needsStepUp, useAdminApi } from "@/lib/api/admin";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
// The admin dashboard's account-and-access actions
// (plans/features/implemented/admin/email-blocks.md, and the actions plan
// after it): a ConfirmationDialog that asks why, then, when the API answers
// that the session needs a fresh code (403 stepUp), asks for one, opens the
// session's step-up window (POST /api/admin/step-up) and tries the action
// again. The reason (10 to 500 characters) is stored with the action's event.
// A wrong code empties the code; a 401 (the session ended, e.g. after its
// fifth wrong code) goes back to the admin sign-in. Key it per opening
// (useOpenCount), so each starts empty.
const minReason = 10;
const maxReason = 500;
// A reason's length as the API counts it: characters, once trimmed.
function reasonLength(reason: string): number {
  return Array.from(reason.trim()).length;
}
export function AdminActionDialog({
  open,
  onClose,
  title,
  description,
  confirm,
  variant = "default",
  action,
  onDone,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description: string;
  // The confirm button's label, e.g. "Unblock".
  confirm: string;
  // "danger" for a destructive action.
  variant?: "danger" | "default";
  // The action itself, given the trimmed reason.
  action: (reason: string) => Promise<unknown>;
  // After the dialog closed on success: a toast, a reload.
  onDone?: () => unknown;
}) {
  const api = useAdminApi();
  const navigate = useNavigate();
  const key = useAdminKey();
  const [reason, setReason] = useState("");
  const [code, setCode] = useState("");
  const [needsCode, setNeedsCode] = useState(false);
  const [errors, setErrors] = useState<{ reason?: string; code?: string }>({});
  const length = reasonLength(reason);
  const ready =
    length >= minReason &&
    length <= maxReason &&
    (!needsCode || code.trim() !== "");
  async function run() {
    setErrors({});
    if (needsCode) {
      await api.stepUp(code);
      // The window is open: a failure of the action itself no longer asks
      // for a code (it would be one already used).
      setNeedsCode(false);
      setCode("");
    }
    await action(reason.trim());
  }
  function failed(error: unknown) {
    if (error instanceof ApiError && error.status === 401) {
      void navigate(adminRoutes.login(key), { replace: true });
      return true;
    }
    if (needsStepUp(error)) {
      setNeedsCode(true);
      setCode("");
      return true;
    }
    if (
      error instanceof ApiError &&
      (error.field === "reason" || error.field === "code")
    ) {
      setErrors({ [error.field]: errorMessage(error) });
      // A refused code is used up or wrong: type a new one.
      if (error.field === "code") setCode("");
      return true;
    }
    return false;
  }
  return (
    <ConfirmationDialog
      open={open}
      onClose={onClose}
      title={title}
      description={description}
      confirm={confirm}
      variant={variant}
      disabled={!ready}
      disabledReason="Fill in required fields"
      onConfirm={run}
      onError={failed}
      onConfirmed={onDone}
    >
      <FieldGroup>
        <Field>
          <Label>Reason</Label>
          <Description>
            Why you’re doing this, kept with the action in the audit trail.{" "}
            {minReason} to {maxReason} characters.
          </Description>
          <Textarea
            name="reason"
            rows={2}
            maxLength={maxReason}
            invalid={Boolean(errors.reason)}
            value={reason}
            onChange={(e) => setReason(e.currentTarget.value)}
          />
          {errors.reason && (
            <ErrorMessage role="alert">{errors.reason}</ErrorMessage>
          )}
          <Text className="mt-1 text-right tabular-nums" aria-hidden="true">
            {length} / {maxReason}
          </Text>
        </Field>
        {needsCode && (
          <AppOrRecoveryCode
            description="Enter a code to continue. It lets you take actions for the next 5 minutes."
            value={code}
            onChange={setCode}
            error={errors.code}
            disabled={false}
            autoFocus
          />
        )}
      </FieldGroup>
    </ConfirmationDialog>
  );
}
