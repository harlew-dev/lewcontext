import { useState } from "react";
import { ShieldCheck, KeyRound, Monitor, CheckCircle2 } from "lucide-react";
import { AppShell } from "@/components/layout/AppShell";
import { PageHeading, SectionHeading } from "@/components/ui/Typography";
import { Button } from "@/components/ui/Button";
import { ErrorNotice, ServiceUnavailable } from "@/components/ui/Feedback";
import { request, errorMessage } from "@/lib/api";
import { apiRoutes } from "@/lib/routes";
import type { WorkspaceProps } from "@/lib/types";
export { workspaceProps as getServerSideProps } from "@/lib/server";
export default function Settings(
  props: WorkspaceProps | { unavailable: true },
) {
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
      <section className="settings-section">
        <SectionHeading
          title="Your profile"
          description="A familiar face in your workspace."
        />
        <dl className="details-list">
          <div>
            <dt>Name</dt>
            <dd>{session.user.name}</dd>
          </div>
          <div>
            <dt>Email address</dt>
            <dd>{session.user.email}</dd>
          </div>
          <div>
            <dt>Member since</dt>
            <dd>
              {new Date(session.user.createdAt).toLocaleDateString("en-US", {
                dateStyle: "long",
                timeZone: "UTC",
              })}
            </dd>
          </div>
        </dl>
      </section>
      <section className="settings-section">
        <SectionHeading
          title="Account security"
          description="A little peace of mind, built in."
        />
        <div className="security-row">
          <span className="security-icon">
            <KeyRound size={20} />
          </span>
          <div>
            <h3>Password protected</h3>
            <p>Your password is stored with a one-way, salted hash.</p>
          </div>
          <span className="badge badge-completed">Protected</span>
        </div>
        <div className="security-row">
          <span className="security-icon">
            <ShieldCheck size={20} />
          </span>
          <div>
            <h3>Private sessions</h3>
            <p>
              You’re automatically signed out after 30 minutes of inactivity.
            </p>
          </div>
        </div>
      </section>
      <section className="settings-section">
        <SectionHeading
          title="Your sign-in sessions"
          description="Stay in control of where you’re signed in."
        />
        <div className="security-row">
          <span className="security-icon">
            <Monitor size={20} />
          </span>
          <div>
            <h3>This browser</h3>
            <p>
              Signed in{" "}
              {new Date(session.signedInAt).toLocaleString("en-US", {
                dateStyle: "medium",
                timeStyle: "short",
                timeZone: "UTC",
              })}{" "}
              UTC
            </p>
          </div>
          <span className="badge badge-in-progress">Current session</span>
        </div>
        <div className="session-action">
          <p>Signed in somewhere else? You can end all other sessions here.</p>
          <Button kind="secondary" isLoading={busy} onClick={revoke}>
            Sign out other sessions
          </Button>
        </div>
        {error && <ErrorNotice message={error} />}
        {done && (
          <div className="success-notice" role="status">
            <CheckCircle2 size={16} />
            All other sessions have been signed out.
          </div>
        )}
      </section>
    </AppShell>
  );
}
