import { useNavigate } from "react-router";
import { AuthLayout } from "@/components/layout/auth-layout";
import { ServiceUnavailable } from "@/components/layout/status-page";
import {
  WorkspaceNameField,
  useCreateWorkspace,
} from "@/components/app/workspaces/workspace-form";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { Logo } from "@/components/ui/logo";
import { Strong, Text } from "@/components/ui/text";
import { Toaster, toast } from "@/components/ui/toast";
import { errorMessage, useApi } from "@/lib/api";
import { onboardingLoader } from "@/lib/loaders";
import { routes } from "@/lib/routes";
import { SessionProvider, useSession } from "@/lib/session";
import type { Route } from "./+types/onboarding";
// Signed in without a workspace yet: create the first one. Accounts that
// already have one go on to /app.
export function clientLoader() {
  return onboardingLoader();
}
export default function Onboarding({ loaderData }: Route.ComponentProps) {
  if ("unavailable" in loaderData) return <ServiceUnavailable />;
  return (
    <SessionProvider session={loaderData.session}>
      <NewWorkspace />
      <Toaster />
    </SessionProvider>
  );
}
function NewWorkspace() {
  const create = useCreateWorkspace();
  return (
    <AuthLayout>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <form onSubmit={create.submit} className="grid grid-cols-1 gap-8">
          <Logo />
          <div className="space-y-2">
            <Heading>New workspace</Heading>
            <Text>
              Projects live in a workspace. Name your first one — you can create
              more later.
            </Text>
          </div>
          {create.error.message && (
            <Notice key={create.error.key} variant="error">
              {create.error.message}
            </Notice>
          )}
          <WorkspaceNameField
            label="Name"
            autoFocus
            value={create.name}
            onChange={create.setName}
            disabled={create.saving}
          />
          <Button
            type="submit"
            className="w-full"
            loading={create.saving}
            disabled={!create.valid}
          >
            Create workspace
          </Button>
        </form>
        <SignedInAs />
      </div>
    </AuthLayout>
  );
}
// Who is signed in, with a way out for a shared computer or the wrong account.
function SignedInAs() {
  const api = useApi();
  const session = useSession();
  const navigate = useNavigate();
  async function signOut() {
    try {
      await api.auth().logout();
      await navigate(routes.login);
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }
  return (
    <div className="flex items-center justify-between gap-4">
      <Text className="min-w-0 truncate">
        Signed in as <Strong>{session.user.email}</Strong>
      </Text>
      <Button plain className="shrink-0" onClick={signOut}>
        Sign out
      </Button>
    </div>
  );
}
