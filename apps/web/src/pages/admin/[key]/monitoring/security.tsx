import type { ReactNode } from "react";
import { AccountLink, IpLink } from "@/components/app/admin/events";
import {
  formatClock,
  formatDay,
  formatEventTime,
} from "@/components/app/admin/event-filters";
import {
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  NoneFound,
  SeriesChart,
  SharedLimitMeter,
  SignalGrid,
  useMonitoringPage,
} from "@/components/app/admin/monitoring";
import { BarList } from "@/components/ui/chart";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Code, Text, TextLink } from "@/components/ui/text";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import type {
  AddressCount,
  DoorOutcome,
  FailedChecksRow,
  MonitoringFlow,
  MonitoringView,
  SecuritySections,
} from "@/types/admin";
// Is anyone attacking sign-in, second factors, password resets or the admin
// door? The signals first, then each attack's review list over the window:
// addresses and emails appear here, admin-only and recorded (the page's one
// read, admin.monitoring.viewed), and every row links to the Events behind
// it.
export default function AdminMonitoringSecurity() {
  const page = useMonitoringPage<SecuritySections>(
    "security",
    adminRoutes.monitoring.security,
  );
  return (
    <>
      <MonitoringHeader
        title="Security"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading security">
        {(view) => <Security view={view} />}
      </MonitoringBody>
    </>
  );
}
type View = MonitoringView<SecuritySections>;
function Security({ view }: { view: View }) {
  return (
    <>
      <SignalGrid className="mt-8" signals={view.signals} />
      <SignIns view={view} />
      <Stuffing view={view} />
      <Guessing view={view} />
      <SuccessAfterFailures view={view} />
      <Takeovers view={view} />
      <Checks view={view} />
      <Flows view={view} />
      <AdminDoor view={view} />
      <Sessions view={view} />
    </>
  );
}
// Links to the Events behind a row, over the page's window.
function useEvents(view: View) {
  const key = useAdminKey();
  const window = view.window.name;
  return {
    ip: (ip: string) =>
      adminRoutes.events.index(key, {
        ip,
        names: ["auth.login_attempted"],
        window,
      }),
    email: (email: string) => adminRoutes.events.index(key, { email, window }),
  };
}
// An hour of the window: "Nov 1, 14:00".
function hour(value: string): string {
  const t = new Date(value);
  return `${formatDay(t)}, ${formatClock(t)}`;
}
function ReviewTable({
  head,
  children,
}: {
  head: string[];
  children: ReactNode;
}) {
  return (
    <Table dense>
      <TableHead>
        <TableRow>
          {head.map((h) => (
            <TableHeader key={h}>{h}</TableHeader>
          ))}
        </TableRow>
      </TableHead>
      <TableBody>{children}</TableBody>
    </Table>
  );
}
const results: Record<string, string> = {
  succeeded: "Signed in",
  mfa_required: "Right password, code asked",
  wrong_password: "Wrong password",
  unknown_account: "Unknown account",
};
function SignIns({ view }: { view: View }) {
  const { attempts, unknown } = view.sections.unknown;
  const share = attempts ? Math.round((unknown * 100) / attempts) : 0;
  return (
    <MonitoringSection
      title="Sign-in attempts"
      description={`By result. ${unknown.toLocaleString("en-US")} of ${attempts.toLocaleString("en-US")} (${share}%) named an email with no account: a list being replayed is mostly misses.`}
    >
      <SeriesChart
        label="Sign-in attempts"
        series={view.sections.signIns}
        labels={results}
        bucketSeconds={view.window.bucketSeconds}
        noun={["attempt", "attempts"]}
      />
    </MonitoringSection>
  );
}
function Stuffing({ view }: { view: View }) {
  const events = useEvents(view);
  const rows = view.sections.stuffing;
  return (
    <MonitoringSection
      title="Credential stuffing"
      description="Addresses (an IPv6 address by its /64) that tried 10 or more different emails in an hour."
    >
      {rows.length === 0 ? (
        <NoneFound>No address tried 10 or more emails in an hour.</NoneFound>
      ) : (
        <ReviewTable
          head={["Hour (UTC)", "Address", "Emails", "Attempts", "Signed in"]}
        >
          {rows.map((r) => (
            <TableRow key={`${r.period}${r.ip}`}>
              <TableCell>{hour(r.period)}</TableCell>
              <TableCell>
                <IpLink ip={r.ip} href={events.ip(r.ip)} />
              </TableCell>
              <TableCell className="tabular-nums">{r.emails}</TableCell>
              <TableCell className="tabular-nums">{r.attempts}</TableCell>
              <TableCell className="tabular-nums">
                {r.successes > 0 ? <strong>{r.successes}</strong> : 0}
              </TableCell>
            </TableRow>
          ))}
        </ReviewTable>
      )}
    </MonitoringSection>
  );
}
function Guessing({ view }: { view: View }) {
  const events = useEvents(view);
  const rows = view.sections.guessing;
  return (
    <MonitoringSection
      title="Distributed guessing"
      description="Emails that failed to sign in from 5 or more addresses in an hour."
    >
      {rows.length === 0 ? (
        <NoneFound>
          No email failed from 5 or more addresses in an hour.
        </NoneFound>
      ) : (
        <ReviewTable head={["Hour (UTC)", "Email", "Addresses", "Failures"]}>
          {rows.map((r) => (
            <TableRow key={`${r.period}${r.email}`}>
              <TableCell>{hour(r.period)}</TableCell>
              <TableCell>
                <TextLink href={events.email(r.email)}>{r.email}</TextLink>
              </TableCell>
              <TableCell className="tabular-nums">{r.addresses}</TableCell>
              <TableCell className="tabular-nums">{r.failures}</TableCell>
            </TableRow>
          ))}
        </ReviewTable>
      )}
    </MonitoringSection>
  );
}
function SuccessAfterFailures({ view }: { view: View }) {
  const rows = view.sections.successAfterFailures;
  return (
    <MonitoringSection
      title="Success after failures"
      description="Accounts signed in (the right password, even if a code was still asked) after 5 or more wrong passwords in an hour: the guess that worked."
    >
      {rows.length === 0 ? (
        <NoneFound>No account signed in after 5 or more failures.</NoneFound>
      ) : (
        <ReviewTable head={["Account", "Wrong passwords", "Signed in (UTC)"]}>
          {rows.map((r) => (
            <TableRow key={`${r.period}${r.account.id}`}>
              <TableCell>
                <AccountLink account={r.account} />
              </TableCell>
              <TableCell className="tabular-nums">{r.failures}</TableCell>
              <TableCell>{formatEventTime(r.signedInAt)}</TableCell>
            </TableRow>
          ))}
        </ReviewTable>
      )}
    </MonitoringSection>
  );
}
const changes: Record<string, string> = {
  "auth.password_changed": "Password changed",
  "auth.mfa.disabled": "Two-factor turned off",
  "auth.recovery_codes.created": "New recovery codes",
  "auth.other_sessions_revoked": "Other sessions signed out",
};
function Takeovers({ view }: { view: View }) {
  const key = useAdminKey();
  const rows = view.sections.takeovers;
  return (
    <MonitoringSection
      title="Possible takeovers"
      description="A sign-in from a network (IPv4 /24, IPv6 /48) the account hadn't used in 30 days, followed within the hour by a change someone taking it over makes first."
    >
      {rows.length === 0 ? (
        <NoneFound>
          No sign-in from a new network was followed by such a change.
        </NoneFound>
      ) : (
        <ReviewTable
          head={["Account", "New network", "Signed in (UTC)", "Then"]}
        >
          {rows.map((r) => (
            <TableRow key={r.event}>
              <TableCell>
                <AccountLink account={r.account} />
              </TableCell>
              <TableCell>
                <Code>{r.network}</Code>
              </TableCell>
              <TableCell>{formatEventTime(r.signedInAt)}</TableCell>
              <TableCell>
                <TextLink href={adminRoutes.events.show(key, r.event)}>
                  {changes[r.change] ?? r.change}
                </TextLink>{" "}
                <span className="text-zinc-500">
                  {formatClock(new Date(r.changedAt))}
                </span>
              </TableCell>
            </TableRow>
          ))}
        </ReviewTable>
      )}
    </MonitoringSection>
  );
}
function FailedChecks({
  rows,
  none,
}: {
  rows: FailedChecksRow[];
  none: string;
}) {
  if (rows.length === 0) return <NoneFound>{none}</NoneFound>;
  return (
    <ReviewTable head={["Hour (UTC)", "Account", "Failures"]}>
      {rows.map((r) => (
        <TableRow key={`${r.period}${r.account.id}`}>
          <TableCell>{hour(r.period)}</TableCell>
          <TableCell>
            <AccountLink account={r.account} />
          </TableCell>
          <TableCell className="tabular-nums">{r.failures}</TableCell>
        </TableRow>
      ))}
    </ReviewTable>
  );
}
function Checks({ view }: { view: View }) {
  const { failures, backupCodes } = view.sections.secondFactor;
  return (
    <div className="grid gap-x-14 lg:grid-cols-2">
      <MonitoringSection
        title="Second factor"
        description={`Accounts failing 3 or more codes in an hour. ${backupCodes.used} recovery ${backupCodes.used === 1 ? "code" : "codes"} used in this window; ${backupCodes.lowAccounts} ${backupCodes.lowAccounts === 1 ? "account has" : "accounts have"} 2 or fewer left.`}
      >
        <FailedChecks
          rows={failures}
          none="No account failed 3 or more codes in an hour."
        />
      </MonitoringSection>
      <MonitoringSection
        title="Password checks"
        description="Signed-in accounts failing their password 3 or more times in an hour, before deleting or turning something off: a stolen session guessing."
      >
        <FailedChecks
          rows={view.sections.reauthentication}
          none="No account failed its password 3 or more times in an hour."
        />
      </MonitoringSection>
    </div>
  );
}
function TopAddresses({
  label,
  rows,
  view,
  names,
}: {
  label: string;
  rows: AddressCount[];
  view: View;
  names: string[];
}) {
  const key = useAdminKey();
  if (rows.length === 0) return <Text>No addresses in this window.</Text>;
  return (
    <BarList
      label={label}
      total={rows.reduce((n, r) => n + r.count, 0)}
      items={rows.map((r) => ({
        key: r.ip,
        label: <span className="font-mono">{r.ip}</span>,
        value: r.count,
        href: adminRoutes.events.index(key, {
          ip: r.ip,
          names,
          window: view.window.name,
        }),
      }))}
    />
  );
}
function Flow({
  title,
  description,
  flow,
  view,
  labels,
  addressesOf,
}: {
  title: string;
  description: string;
  flow: MonitoringFlow;
  view: View;
  labels: Record<string, string>;
  addressesOf: string;
}) {
  return (
    <MonitoringSection title={title} description={description}>
      <SeriesChart
        label={title}
        series={flow.series}
        labels={labels}
        bucketSeconds={view.window.bucketSeconds}
        noun={["event", "events"]}
      />
      <div className="mt-6 grid gap-8 sm:grid-cols-2">
        <div>
          <Text>
            <strong>Most from one address</strong>
          </Text>
          <TopAddresses
            label={`${title}: top addresses`}
            rows={flow.topAddresses}
            view={view}
            names={[addressesOf]}
          />
        </div>
        {flow.limit && <SharedLimitMeter limit={flow.limit} />}
      </div>
    </MonitoringSection>
  );
}
function Flows({ view }: { view: View }) {
  return (
    <>
      <Flow
        title="Password resets"
        description="Asked for against sent: a reset for an email with no account sends nothing, so a wide gap is someone trying addresses."
        flow={view.sections.resets}
        view={view}
        labels={{
          "auth.password_reset.requested": "Asked for",
          "auth.password_reset.sent": "Sent",
        }}
        addressesOf="auth.password_reset.requested"
      />
      <Flow
        title="Sign-ups"
        description="Codes asked for and accounts made; the addresses that made the most accounts."
        flow={view.sections.signUps}
        view={view}
        labels={{
          "auth.registration.requested": "Code asked for",
          "auth.registered": "Account made",
        }}
        addressesOf="auth.registered"
      />
    </>
  );
}
const doorLabels: Record<DoorOutcome, string> = {
  wrong_password: "Wrong password",
  unknown_account: "Unknown account",
  not_admin: "Not an admin",
  mfa_off: "Admin without MFA",
  access_refused: "Address not allowed",
  session_moved: "Session moved network",
};
function AdminDoor({ view }: { view: View }) {
  const key = useAdminKey();
  const { counts, series, recent } = view.sections.adminDoor;
  return (
    <MonitoringSection
      title="Admin door"
      description="Failed admin sign-ins, refusals of real credentials or of the key from elsewhere (someone holds the key, and maybe a password), and sessions used from another network."
    >
      <dl className="grid grid-cols-2 gap-x-8 gap-y-2 text-sm/6 sm:grid-cols-3">
        {counts.map((c) => (
          <div key={c.outcome} className="flex justify-between gap-2">
            <dt className="text-zinc-500 dark:text-zinc-400">
              {doorLabels[c.outcome]}
            </dt>
            <dd className="font-medium tabular-nums">{c.count}</dd>
          </div>
        ))}
      </dl>
      <div className="mt-6">
        <SeriesChart
          label="Admin door"
          series={series}
          labels={doorLabels}
          bucketSeconds={view.window.bucketSeconds}
          noun={["event", "events"]}
        />
      </div>
      {recent.length > 0 && (
        <div className="mt-6">
          <ReviewTable head={["Time (UTC)", "What", "Address"]}>
            {recent.map((r) => (
              <TableRow key={r.event}>
                <TableCell>
                  <TextLink href={adminRoutes.events.show(key, r.event)}>
                    {formatEventTime(r.at)}
                  </TextLink>
                </TableCell>
                <TableCell>{doorLabels[r.outcome]}</TableCell>
                <TableCell>{r.ip ? <IpLink ip={r.ip} /> : "–"}</TableCell>
              </TableRow>
            ))}
          </ReviewTable>
        </div>
      )}
    </MonitoringSection>
  );
}
function Sessions({ view }: { view: View }) {
  const { series, live, accountsAtCap } = view.sections.sessions;
  return (
    <MonitoringSection
      title="Sessions"
      description={`${live.toLocaleString("en-US")} live app ${live === 1 ? "session" : "sessions"} now; ${accountsAtCap} ${accountsAtCap === 1 ? "account is" : "accounts are"} at the 5-session cap.`}
    >
      <SeriesChart
        label="Sign-ins"
        series={series}
        labels={{ "auth.logged_in": "Sign-ins" }}
        bucketSeconds={view.window.bucketSeconds}
        noun={["sign-in", "sign-ins"]}
      />
    </MonitoringSection>
  );
}
