import { useState, type ReactNode } from "react";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
import { toast } from "@/components/ui/toast";
import { useApi } from "@/lib/api";
import type { ApiKey } from "@/types/api-keys";
import type { Workspace } from "@/types/workspaces";
import { KeyDialog } from "./key-dialog";
// Editing and revoking a key, from the list's ⋯ menu and from a key's own
// page: the dialogs, and what opens them. `workspace` is the workspace being
// managed, or none for the account's own keys. `changed` runs once a key was
// saved or revoked, to reload what shows it.
export function useKeyActions(
  workspace: Workspace | undefined,
  changed: () => void,
) {
  const api = useApi();
  const [dialog, setDialog] = useState<{ editing?: ApiKey } | null>(null);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);
  const dialogs: ReactNode = (
    <>
      {dialog && (
        <KeyDialog
          workspace={workspace}
          editing={dialog.editing}
          onClose={() => setDialog(null)}
          onSaved={changed}
        />
      )}
      {revoking && (
        <ConfirmationDialog
          open
          onClose={() => setRevoking(null)}
          title={`Revoke ${revoking.name}?`}
          description="Integrations using this key will stop working immediately."
          confirm="Revoke API key"
          variant="danger"
          onConfirm={() => api.apiKeys(workspace?.id).destroy(revoking.id)}
          onConfirmed={() => {
            setRevoking(null);
            changed();
            toast.success("API key revoked.");
          }}
        />
      )}
    </>
  );
  return {
    create: () => setDialog({}),
    edit: (key: ApiKey) => setDialog({ editing: key }),
    revoke: (key: ApiKey) => setRevoking(key),
    dialogs,
  };
}
