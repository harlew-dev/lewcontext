import { KeyList } from "@/components/app/api-keys/key-list";
import { Text } from "@/components/ui/text";
import { useWorkspace } from "@/lib/workspace";

export default function UserApiKeyIndex() {
  const workspace = useWorkspace();
  return (
    <div key={workspace.id} className="space-y-6">
      <Text>
        Personal API keys are for your scripts. Each belongs to one workspace
        and can do only what your current role permits. Suspension or leaving
        the workspace permanently revokes them. Password changes and browser
        sign-out do not.
      </Text>
      <KeyList title="Personal API keys" />
    </div>
  );
}
