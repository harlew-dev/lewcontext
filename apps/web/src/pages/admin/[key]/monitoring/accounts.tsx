import type { ReactNode } from "react";
import { AccountLink, WorkspaceLink } from "@/components/app/admin/events";
import { formatEventTime } from "@/components/app/admin/event-filters";
import {
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  MonitoringTable,
  NoneFound,
  SectionGate,
  SignalGrid,
  useMonitoringPage,
} from "@/components/app/admin/monitoring";
import { Badge } from "@/components/ui/badge";
import { TableCell, TableRow } from "@/components/ui/table";
import { TextLink } from "@/components/ui/text";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import type {
  AccountCount,
  AccountReview,
  AccountsSections,
  MonitoringView,
} from "@/types/admin";
// Which accounts behave like spam or abuse? A review list of the accounts
// tripping any signal in a day of the window, each with every signal it
// tripped (accounts by ID and name, never email: their page has it, and
// opening it is its own recorded read); then the accounts writing the most
// and owning the most workspaces. One request, one recorded read.
//
// TODO(A6, actions): the account actions (sign out everywhere, reset MFA,
// suspend) live on the account's own page, users/[user].tsx, once the
// actions branch is merged; a row here links there. If A6 wants them
// in the list too, they go in a last column of ReviewRow.
export default function AdminMonitoringAccounts() {
  const page = useMonitoringPage<AccountsSections>(
    "accounts",
    adminRoutes.monitoring.accounts,
  );
  return (
    <>
      <MonitoringHeader
        title="Accounts"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading accounts">
        {(view) => <Accounts view={view} />}
      </MonitoringBody>
    </>
  );
}
type View = MonitoringView<AccountsSections>;
function Accounts({ view }: { view: View }) {
  return (
    <>
      <SignalGrid className="mt-8" signals={view.signals} />
      <SectionGate names={["review"]} title="Accounts to review">
        <Review view={view} />
      </SectionGate>
      <div className="grid gap-x-14 lg:grid-cols-2">
        <SectionGate names={["topWriters"]} title="Writing the most">
          <Top
            title="Writing the most"
            description="Events about workspaces and projects each account recorded."
            rows={view.sections.topWriters}
            noun="events"
            empty="No account wrote anything in this window."
          />
        </SectionGate>
        <SectionGate names={["topOwners"]} title="Owning the most workspaces">
          <Top
            title="Owning the most workspaces"
            description="Of the accounts that made a workspace in this window, how many each owns (20 at most)."
            rows={view.sections.topOwners}
            noun="workspaces"
            empty="No account made a workspace in this window."
          />
        </SectionGate>
      </div>
    </>
  );
}
function Review({ view }: { view: View }) {
  const rows = view.sections.review;
  return (
    <MonitoringSection
      title="Accounts to review"
      description="Accounts tripping any signal in a day of this window (acceptance over the last 7 days): most signals first, then new accounts (made in the last 24 hours)."
    >
      {rows.length === 0 ? (
        <NoneFound>No account tripped a signal in this window.</NoneFound>
      ) : (
        <MonitoringTable head={["Account", "Signals", "Made (UTC)", "Events"]}>
          {rows.map((r) => (
            <ReviewRow key={r.account.id} row={r} view={view} />
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
function ReviewRow({ row, view }: { row: AccountReview; view: View }) {
  const key = useAdminKey();
  return (
    <TableRow>
      <TableCell>
        <span className="flex flex-wrap items-center gap-2">
          <AccountLink account={row.account} />
          {row.new && <Badge color="pink">New</Badge>}
        </span>
      </TableCell>
      <TableCell>
        <ul className="space-y-1">
          {row.signals.map((s, i) => (
            <li key={`${s.signal}${i}`}>{signalText(s)}</li>
          ))}
        </ul>
      </TableCell>
      <TableCell>{formatEventTime(row.createdAt)}</TableCell>
      <TableCell>
        <TextLink
          href={adminRoutes.events.index(key, {
            account: row.account.id,
            window: view.window.name,
          })}
        >
          Activity
        </TextLink>
      </TableCell>
    </TableRow>
  );
}
// A tripped signal in words: "5 workspaces made in a day".
function signalText(s: AccountReview["signals"][number]): ReactNode {
  const n = s.count.toLocaleString("en-US");
  switch (s.signal) {
    case "workspaces":
      return `${n} workspaces made in a day`;
    case "invitations":
      return `${n} invitations to ${s.of ?? 0} addresses in a day`;
    case "acceptance":
      return `${n} of ${s.of ?? 0} invitations accepted in 7 days`;
    case "projects":
      return `${n} projects made in a day`;
    case "limits":
      return `${n} limit ${s.count === 1 ? "window" : "windows"} reached in a day`;
    case "checks":
      return `${n} failed password or code checks in a day`;
    case "links":
      return s.workspace ? (
        <>
          A link in its workspace&rsquo;s name:{" "}
          <WorkspaceLink
            workspace={{ id: s.workspace.id, name: s.workspace.name }}
          />
        </>
      ) : (
        "A link or markup in its name"
      );
  }
}
function Top({
  title,
  description,
  rows,
  noun,
  empty,
}: {
  title: string;
  description: string;
  rows: AccountCount[];
  noun: string;
  empty: string;
}) {
  return (
    <MonitoringSection title={title} description={description}>
      {rows.length === 0 ? (
        <NoneFound>{empty}</NoneFound>
      ) : (
        <MonitoringTable
          head={["Account", noun[0].toUpperCase() + noun.slice(1)]}
        >
          {rows.map((r) => (
            <TableRow key={r.account.id}>
              <TableCell>
                <AccountLink account={r.account} />
              </TableCell>
              <TableCell className="tabular-nums">
                {r.count.toLocaleString("en-US")}
              </TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
