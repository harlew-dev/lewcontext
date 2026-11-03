import { useState } from "react";
import { useNavigate } from "react-router";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
import {
  ApiError,
  errorMessage,
  lastOwnerWorkspaces,
  useApi,
  type WorkspaceSummary,
} from "@/lib/api";
import { routes, type AuthPageState } from "@/lib/routes";
import { useSession } from "@/lib/session";
import { useClearCache } from "@/lib/use-clear-cache";
import { PasswordField } from "./password-fields";
// Deleting the account, from account settings and from onboarding (an
// account with no workspace can't open settings). Your password confirms it.
// Afterwards the SWR cache is cleared, so none of the account's data stays in
// memory, and login says the account is gone. The only owner of a workspace
// is refused (409): `onLastOwner` gets those workspaces, and without it the
// refusal shows in the dialog.
export function DeleteAccountDialog({
  open,
  workspaces,
  onClose,
  onLastOwner,
}: {
  open: boolean;
  // How many workspaces you're in, once loaded.
  workspaces?: number;
  onClose: () => void;
  onLastOwner?: (workspaces: WorkspaceSummary[]) => void;
}) {
  const api = useApi();
  const session = useSession();
  const navigate = useNavigate();
  const clearCache = useClearCache();
  const [password, setPassword] = useState("");
  const [passwordError, setPasswordError] = useState<string>();
  function showElsewhere(e: unknown): boolean {
    const blocking = lastOwnerWorkspaces(e);
    if (blocking && onLastOwner) {
      onLastOwner(blocking);
      return true;
    }
    if (e instanceof ApiError && e.field === "password") {
      setPasswordError(errorMessage(e));
      return true;
    }
    return false;
  }
  async function destroy() {
    setPasswordError(undefined);
    await api.auth().destroy({ password });
    const state: AuthPageState = { accountDeleted: true };
    await navigate(routes.login, { replace: true, state });
    clearCache();
  }
  return (
    <ConfirmationDialog
      open={open}
      onClose={onClose}
      title="Delete your account?"
      description={accountDeletionEffects(workspaces)}
      confirm="Delete account"
      variant="danger"
      disabled={password === ""}
      onConfirm={destroy}
      onError={showElsewhere}
    >
      {/* Tells password managers which account the password belongs to. */}
      <input
        type="email"
        name="username"
        autoComplete="username"
        value={session.user.email}
        readOnly
        hidden
      />
      <PasswordField
        label="Your password"
        autoComplete="current-password"
        value={password}
        onChange={setPassword}
        error={passwordError}
        disabled={false}
      />
    </ConfirmationDialog>
  );
}
function accountDeletionEffects(workspaces?: number): string {
  const after = "It can’t be undone.";
  if (workspaces === 0)
    return `This permanently deletes your account. ${after}`;
  const from =
    workspaces === undefined
      ? "every workspace you’re in"
      : `${workspaces} ${workspaces === 1 ? "workspace" : "workspaces"}`;
  return `This permanently deletes your account and removes you from ${from}. ${after}`;
}
