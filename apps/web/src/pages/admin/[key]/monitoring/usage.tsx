import { AdminBadge } from "@/components/app/admin/admin-list";
import { AccountLink } from "@/components/app/admin/events";
import { formatDay } from "@/components/app/admin/event-filters";
import {
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  MonitoringTable,
  NoneFound,
  SectionGate,
  SeriesChart,
  SignalGrid,
  useMonitoringPage,
} from "@/components/app/admin/monitoring";
import { Stat } from "@/components/ui/stat";
import { TableCell, TableRow } from "@/components/ui/table";
import { Text } from "@/components/ui/text";
import { adminRoutes } from "@/lib/admin-routes";
import type {
  Activation,
  MonitoringView,
  SizeCount,
  UsageSections,
} from "@/types/admin";
// How many people use the app, and what do they use? Active accounts (from
// last seen, moved at most hourly), sign-ups and how many get going,
// feature use, workspace sizes, and the accounts not seen for two years
// (listed, never deleted automatically: deleting them is a business
// policy). Counts throughout; accounts by ID and name only. One request,
// one recorded read.
export default function AdminMonitoringUsage() {
  const page = useMonitoringPage<UsageSections>(
    "usage",
    adminRoutes.monitoring.usage,
  );
  return (
    <>
      <MonitoringHeader
        title="Usage"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading usage">
        {(view) => <Usage view={view} />}
      </MonitoringBody>
    </>
  );
}
type View = MonitoringView<UsageSections>;
function Usage({ view }: { view: View }) {
  return (
    <>
      <SignalGrid className="mt-8" signals={view.signals} />
      <SectionGate names={["active"]} title="Active accounts">
        <Active view={view} />
      </SectionGate>
      <SectionGate names={["signUps", "activation"]} title="Sign-ups">
        <SignUps view={view} />
      </SectionGate>
      <SectionGate names={["features"]} title="Feature use">
        <Features view={view} />
      </SectionGate>
      <SectionGate names={["sizes"]} title="Workspace sizes">
        <Sizes view={view} />
      </SectionGate>
      <SectionGate names={["inactive"]} title="Not seen for two years">
        <Inactive view={view} />
      </SectionGate>
    </>
  );
}
const count = (n: number) => n.toLocaleString("en-US");
const percent = (part: number, whole: number) =>
  whole ? `${Math.round((part * 100) / whole)}%` : "–";
function Active({ view }: { view: View }) {
  const { day, week, month, withMfa } = view.sections.active;
  return (
    <MonitoringSection
      title="Active accounts"
      description="Accounts with a signed-in request, from their last seen time (moved at most once an hour)."
    >
      <div className="grid gap-8 sm:grid-cols-2 xl:grid-cols-4">
        <Stat title="Last 24 hours" value={count(day)} />
        <Stat title="Last 7 days" value={count(week)} />
        <Stat title="Last 30 days" value={count(month)} />
        <Stat title="With two-factor" value={percent(withMfa, week)}>
          <span className="text-zinc-500 dark:text-zinc-400">
            {count(withMfa)} of the last 7 days&rsquo; {count(week)}
          </span>
        </Stat>
      </div>
    </MonitoringSection>
  );
}
// "2 of 4 (50%) of the accounts made Oct 24 – Oct 31".
function activationText(a: Activation, did: string): string {
  const from = formatDay(new Date(a.from));
  const to = formatDay(new Date(a.to));
  if (a.accounts === 0) return `No account was made ${from} – ${to}.`;
  return `${count(a.activated)} of the ${count(a.accounts)} accounts made ${from} – ${to} (${percent(a.activated, a.accounts)}) ${did}.`;
}
function SignUps({ view }: { view: View }) {
  const { signUps, activation } = view.sections;
  return (
    <MonitoringSection
      title="Sign-ups"
      description="Accounts made over the window, and how many of the last weeks' got going: each given its whole day, or week, to do it."
    >
      <SeriesChart
        label="Sign-ups"
        series={signUps}
        labels={{ "auth.registered": "Sign-ups" }}
        bucketSeconds={view.window.bucketSeconds}
        noun={["sign-up", "sign-ups"]}
      />
      <ul className="mt-6 space-y-1 text-sm/6">
        <li>
          {activationText(
            activation.workspace,
            "made or joined a workspace within a day",
          )}
        </li>
        <li>
          {activationText(activation.invited, "invited someone within a week")}
        </li>
      </ul>
    </MonitoringSection>
  );
}
const featureLabels = {
  "project.created": "Projects made",
  "project.updated": "Projects changed",
  "workspace.invitation.created": "Invitations sent",
  "workspace.invitation.accepted": "Invitations accepted",
  "auth.mfa.enabled": "Two-factor turned on",
};
function Features({ view }: { view: View }) {
  return (
    <MonitoringSection
      title="Feature use"
      description="What people did, per bucket of the window."
    >
      <SeriesChart
        label="Feature use"
        series={view.sections.features}
        labels={featureLabels}
        bucketSeconds={view.window.bucketSeconds}
        noun={["event", "events"]}
      />
    </MonitoringSection>
  );
}
function SizeTable({ label, rows }: { label: string; rows: SizeCount[] }) {
  return (
    <MonitoringTable head={[label, "Workspaces"]}>
      {rows.map((r) => (
        <TableRow key={r.size}>
          <TableCell className="tabular-nums">{r.size}</TableCell>
          <TableCell className="tabular-nums">{count(r.workspaces)}</TableCell>
        </TableRow>
      ))}
    </MonitoringTable>
  );
}
function Sizes({ view }: { view: View }) {
  const { members, projects } = view.sections.sizes;
  return (
    <MonitoringSection
      title="Workspace sizes"
      description="How big the workspaces with an event in this window are."
    >
      <div className="grid gap-x-14 gap-y-8 lg:grid-cols-2">
        <SizeTable label="Members" rows={members} />
        <SizeTable label="Projects" rows={projects} />
      </div>
    </MonitoringSection>
  );
}
function Inactive({ view }: { view: View }) {
  const { count: total, accounts, before } = view.sections.inactive;
  return (
    <MonitoringSection
      title="Not seen for two years"
      description={`Accounts with no signed-in request since ${formatDay(new Date(before))}, ${new Date(before).getUTCFullYear()} (or never, made before then). Listed for a decision, never deleted automatically: deleting them is a business policy, not a default.`}
    >
      {accounts.length === 0 ? (
        <NoneFound>
          Every account has been seen in the last two years.
        </NoneFound>
      ) : (
        <>
          <Text className="mb-4">
            {count(total)} {total === 1 ? "account" : "accounts"}
            {total > accounts.length &&
              `; the ${accounts.length} unseen longest below`}
            .
          </Text>
          <MonitoringTable head={["Account", "Last seen", "Made"]}>
            {accounts.map((a) => (
              <TableRow key={a.account.id}>
                <TableCell>
                  <span className="flex flex-wrap items-center gap-2">
                    <AccountLink account={a.account} />
                    {a.admin && <AdminBadge />}
                  </span>
                </TableCell>
                <TableCell>
                  {a.lastSeenAt ? utcDate(a.lastSeenAt) : "Never"}
                </TableCell>
                <TableCell>{utcDate(a.createdAt)}</TableCell>
              </TableRow>
            ))}
          </MonitoringTable>
        </>
      )}
    </MonitoringSection>
  );
}
// "Oct 31, 2023", in UTC.
function utcDate(value: string): string {
  const d = new Date(value);
  return `${formatDay(d)}, ${d.getUTCFullYear()}`;
}
