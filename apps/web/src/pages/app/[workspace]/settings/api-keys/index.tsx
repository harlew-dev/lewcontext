import { KeyList } from "@/components/app/api-keys/key-list";
import { Notice } from "@/components/ui/feedback";
import { Text } from "@/components/ui/text";
import { useWorkspace } from "@/lib/workspace";
import { can } from "@/types/workspaces";

export default function Index() {
  const workspace = useWorkspace();
  if (!can(workspace, "workspace.api_key.index"))
    return (
      <Notice variant="error">
        Only owners and admins can manage this workspace’s API keys.
      </Notice>
    );
  return (
    <div key={workspace.id} className="space-y-10">
      <Text>
        Workspace credentials are for shared integrations and survive staff
        changes. Personal credentials belong to their member and are limited by
        their current access; managers can review and revoke them here.
      </Text>
      <KeyList
        workspace={workspace}
        kind="workspace"
        title="Workspace API keys"
      />
      <KeyList
        workspace={workspace}
        kind="personal"
        title="Personal keys in this workspace"
        allowCreate={false}
      />
    </div>
  );
}
