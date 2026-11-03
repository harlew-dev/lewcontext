import { useParams } from "react-router";
import { KeyPage } from "@/components/app/api-keys/key-page";
import { Notice } from "@/components/ui/feedback";
import { parsePublicId } from "@/lib/ids";
import { useWorkspace } from "@/lib/workspace";
import { can } from "@/types/workspaces";
// A workspace API key's page: owners and admins only (workspace.api_key.show).
export default function WorkspaceApiKeyShow() {
  const workspace = useWorkspace();
  const id = parsePublicId("apk", useParams().key);
  if (!can(workspace, "workspace.api_key.show"))
    return (
      <Notice variant="error">
        Only owners and admins can see this workspace’s API keys.
      </Notice>
    );
  return (
    <KeyPage
      key={`${workspace.id}-${id}`}
      id={id ?? "missing"}
      workspace={workspace}
    />
  );
}
