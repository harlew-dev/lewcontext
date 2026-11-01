import { useState } from "react";
import { useLocation, useNavigate } from "react-router";
import {
  ArrowRightStartOnRectangleIcon,
  EllipsisHorizontalIcon,
  TrashIcon,
} from "@heroicons/react/16/solid";
import { DeleteAccountDialog } from "@/components/app/account/delete-account-dialog";
import { AuthLayout } from "@/components/layout/auth-layout";
import { ServiceUnavailable } from "@/components/layout/status-page";
import {
  WorkspaceNameField,
  useCreateWorkspace,
} from "@/components/app/workspaces/workspace-form";
import { Button } from "@/components/ui/button";
import {
  Dropdown,
  DropdownButton,
  DropdownDivider,
  DropdownHeader,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { Logo } from "@/components/ui/logo";
import { Strong, Text } from "@/components/ui/text";
import { Toaster, toast } from "@/components/ui/toast";
import { errorMessage, useApi } from "@/lib/api";
import { onboardingLoader } from "@/lib/loaders";
import { onboardingState, routes } from "@/lib/routes";
import { useClearCache } from "@/lib/use-clear-cache";
import { SessionProvider, useSession } from "@/lib/session";
import type { Route } from "./+types/onboarding";
// Signed in without a workspace yet: create the first one. Accounts that
// already have one go on to /app.
export function clientLoader() {
  return onboardingLoader();
}
export default function Onboarding({ loaderData }: Route.ComponentProps) {
  if ("unavailable" in loaderData)
    return <ServiceUnavailable requestId={loaderData.requestId} />;
  return (
    <SessionProvider session={loaderData.session}>
      <NewWorkspace />
      <Toaster />
    </SessionProvider>
  );
}
function NewWorkspace() {
  const create = useCreateWorkspace();
  // Workspace settings send you here after deleting or leaving your last
  // workspace.
  const { deletedWorkspace, leftWorkspace } = onboardingState(
    useLocation().state,
  );
  return (
    <AuthLayout>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <div className="flex items-center justify-between gap-4">
          <Logo />
          <AccountMenu />
        </div>
        <form onSubmit={create.submit} className="grid grid-cols-1 gap-8">
          {deletedWorkspace && (
            <Notice variant="success">{deletedWorkspace} deleted.</Notice>
          )}
          {leftWorkspace && (
            <Notice variant="success">You left {leftWorkspace}.</Notice>
          )}
          <div className="space-y-2">
            <Heading>New workspace</Heading>
            <Text>
              Projects live in a workspace. Name your first one — you can create
              more later.
            </Text>
          </div>
          <ErrorNotice error={create.error} />
          <WorkspaceNameField
            label="Name"
            autoFocus
            value={create.name}
            onChange={create.setName}
            disabled={create.loading}
          />
          <Button
            type="submit"
            className="w-full"
            loading={create.loading}
            disabled={!create.valid}
            disabledReason="Fill in required fields"
          >
            Create workspace
          </Button>
        </form>
      </div>
    </AuthLayout>
  );
}
// Who is signed in, with a way out for a shared computer or the wrong
// account: signing out, or deleting the account. Account settings need a
// workspace, so an account without one deletes itself from here.
function AccountMenu() {
  const api = useApi();
  const session = useSession();
  const navigate = useNavigate();
  const clearCache = useClearCache();
  const [deleting, setDeleting] = useState(false);
  // Each opening starts with an empty password.
  const [opened, setOpened] = useState(0);
  async function signOut() {
    try {
      await api.auth().logout();
      await navigate(routes.login);
      clearCache();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }
  return (
    <>
      <Dropdown>
        <DropdownButton plain aria-label="Account">
          <EllipsisHorizontalIcon />
        </DropdownButton>
        <DropdownMenu className="min-w-64" anchor="bottom end">
          <DropdownHeader>
            <Text className="truncate">
              Signed in as <Strong>{session.user.email}</Strong>
            </Text>
          </DropdownHeader>
          <DropdownDivider />
          <DropdownItem variant="danger" onClick={signOut}>
            <ArrowRightStartOnRectangleIcon />
            <DropdownLabel>Sign out</DropdownLabel>
          </DropdownItem>
          <DropdownDivider />
          <DropdownItem
            variant="danger"
            onClick={() => {
              setOpened(opened + 1);
              setDeleting(true);
            }}
          >
            <TrashIcon />
            <DropdownLabel>Delete account</DropdownLabel>
          </DropdownItem>
        </DropdownMenu>
      </Dropdown>
      <DeleteAccountDialog
        key={opened}
        open={deleting}
        workspaces={0}
        onClose={() => setDeleting(false)}
      />
    </>
  );
}
