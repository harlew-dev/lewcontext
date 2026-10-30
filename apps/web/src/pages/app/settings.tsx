import { useState, type ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { ShieldCheck, KeyRound, Monitor } from "lucide-react";
import { AppShell } from "@/components/layout/AppShell";
import { PageHeading, SectionHeading } from "@/components/ui/Typography";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Notice, ServiceUnavailable } from "@/components/ui/Feedback";
import { request, errorMessage } from "@/lib/api";
import { apiRoutes } from "@/lib/routes";
import type { WorkspacePageProps, WorkspaceProps } from "@/lib/types";
export { workspaceProps as getServerSideProps } from "@/lib/server";
export default function Settings(props: WorkspacePageProps) {
  if ("unavailable" in props) return <ServiceUnavailable />;
  return <AccountSettings {...props} />;
}
function AccountSettings({ session }: WorkspaceProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  async function revoke() {
    setBusy(true);
    setError("");
    setDone(false);
    try {
      await request(
        apiRoutes.sessions,
        { method: "DELETE" },
        session.csrfToken,
      );
      setDone(true);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <AppShell session={session} title="Account & security">
      <PageHeading
        eyebrow="YOUR SPACE, YOUR CONTROL"
        title="Make yourself at home."
        description="Your profile and the small details that keep your workspace safe."
      />
      <SettingsSection
        title="Your profile"
        description="A familiar face in your workspace."
      >
        <DetailsList
          items={[
            { label: "Name", value: session.user.name },
            { label: "Email address", value: session.user.email },
            {
              label: "Member since",
              value: new Date(session.user.createdAt).toLocaleDateString(
                "en-US",
                { dateStyle: "long", timeZone: "UTC" },
              ),
            },
          ]}
        />
      </SettingsSection>
      <SettingsSection
        title="Account security"
        description="A little peace of mind, built in."
      >
        <SecurityRow
          icon={KeyRound}
          title="Password protected"
          description="Your password is stored with a one-way, salted hash."
          badge={<Badge variant="completed">Protected</Badge>}
        />
        <SecurityRow
          icon={ShieldCheck}
          title="Private sessions"
          description="You’re automatically signed out after 30 minutes of inactivity."
        />
      </SettingsSection>
      <SettingsSection
        title="Your sign-in sessions"
        description="Stay in control of where you’re signed in."
      >
        <SecurityRow
          icon={Monitor}
          title="This browser"
          description={`Signed in ${new Date(session.signedInAt).toLocaleString(
            "en-US",
            { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" },
          )} UTC`}
          badge={<Badge variant="progress">Current session</Badge>}
        />
        <div className="flex items-center justify-between gap-5 border-t border-line pt-5 max-sm:flex-col max-sm:items-start">
          <p className="text-small leading-[1.6] text-muted">
            Signed in somewhere else? You can end all other sessions here.
          </p>
          <Button kind="secondary" isLoading={busy} onClick={revoke}>
            Sign out other sessions
          </Button>
        </div>
        {error && <Notice variant="error">{error}</Notice>}
        {done && (
          <Notice variant="success" className="mt-4.5">
            All other sessions have been signed out.
          </Notice>
        )}
      </SettingsSection>
    </AppShell>
  );
}
function SettingsSection({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section className="mt-8.75 max-w-[850px] border-t border-line pt-6.25 max-sm:mt-6.75 max-sm:pt-5">
      <SectionHeading title={title} description={description} />
      {children}
    </section>
  );
}
function DetailsList({
  items,
}: {
  items: { label: string; value: ReactNode }[];
}) {
  return (
    <dl className="mt-6.25">
      {items.map(({ label, value }) => (
        <div
          key={label}
          className="grid grid-cols-[180px_1fr] py-3.75 text-body max-sm:grid-cols-[110px_1fr] max-sm:text-small"
        >
          <dt className="text-muted">{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}
function SecurityRow({
  icon: Icon,
  title,
  description,
  badge,
}: {
  icon: LucideIcon;
  title: string;
  description: ReactNode;
  badge?: ReactNode;
}) {
  return (
    <div className="flex items-center gap-4 py-5 max-sm:flex-wrap max-sm:gap-2.5">
      <span className="rounded-lg bg-mauve-100 p-3 text-accent max-sm:p-2.25">
        <Icon size={20} />
      </span>
      <div>
        <h3 className="max-sm:text-small">{title}</h3>
        <p className="mt-1.75 text-small leading-[1.6] text-muted max-sm:max-w-[220px] max-sm:text-label">
          {description}
        </p>
      </div>
      {badge && <div className="ml-auto max-sm:ml-11.25">{badge}</div>}
    </div>
  );
}
