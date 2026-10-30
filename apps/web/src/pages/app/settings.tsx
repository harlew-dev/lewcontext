import { useState } from "react";
import { appLayout } from "@/components/layout/application-layout";
import { ServiceUnavailable } from "@/components/layout/status-page";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import { Divider } from "@/components/ui/divider";
import { Notice } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { SettingsSection } from "@/components/ui/settings-section";
import { Text } from "@/components/ui/text";
import { useApi } from "@/lib/api";
import { useSession } from "@/lib/session";
import type { AppPageProps } from "@/lib/types";
import { useActionError } from "@/lib/use-action-error";
export { sessionProps as getServerSideProps } from "@/lib/server";
export default function Settings(props: AppPageProps) {
  if ("unavailable" in props) return <ServiceUnavailable />;
  return <AccountSettings />;
}
Settings.getLayout = appLayout;
function AccountSettings() {
  const session = useSession();
  return (
    <div className="mx-auto max-w-4xl">
      <Heading>Account & security</Heading>
      <Divider className="my-10 mt-6" />
      <SettingsSection
        title="Profile"
        description="The name and email address on your account."
      >
        <DescriptionList>
          <DescriptionTerm>Name</DescriptionTerm>
          <DescriptionDetails>{session.user.name}</DescriptionDetails>
          <DescriptionTerm>Email address</DescriptionTerm>
          <DescriptionDetails>{session.user.email}</DescriptionDetails>
          <DescriptionTerm>Member since</DescriptionTerm>
          <DescriptionDetails>
            {new Date(session.user.createdAt).toLocaleDateString("en-US", {
              dateStyle: "long",
              timeZone: "UTC",
            })}
          </DescriptionDetails>
        </DescriptionList>
      </SettingsSection>
      <Divider className="my-10" soft />
      <SettingsSection
        title="Password"
        description="Your password is stored with a one-way, salted hash."
      >
        <div>
          <Badge color="lime">Protected</Badge>
        </div>
      </SettingsSection>
      <Divider className="my-10" soft />
      <SettingsSection
        title="Sign-in sessions"
        description="You’re signed out automatically after 30 minutes of inactivity."
      >
        <SessionControls />
      </SettingsSection>
    </div>
  );
}
function SessionControls() {
  const api = useApi();
  const session = useSession();
  const [busy, setBusy] = useState(false);
  const error = useActionError();
  const [done, setDone] = useState(false);
  async function revokeOthers() {
    setBusy(true);
    try {
      await api.sessions().destroyOthers();
      error.clear();
      setDone(true);
    } catch (e) {
      error.fail(e);
      setDone(false);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <div className="text-base/6 font-medium text-zinc-950 sm:text-sm/6 dark:text-white">
            This browser
          </div>
          <Text>
            Signed in{" "}
            {new Date(session.signedInAt).toLocaleString("en-US", {
              dateStyle: "medium",
              timeStyle: "short",
              timeZone: "UTC",
            })}{" "}
            UTC
          </Text>
        </div>
        <Badge color="purple">Current session</Badge>
      </div>
      <Button outline loading={busy} onClick={revokeOthers}>
        Sign out other sessions
      </Button>
      {error.message && (
        <Notice key={error.key} variant="error">
          {error.message}
        </Notice>
      )}
      {done && (
        <Notice variant="success">
          All other sessions have been signed out.
        </Notice>
      )}
    </div>
  );
}
