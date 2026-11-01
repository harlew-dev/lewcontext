import { useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { AccountLink } from "@/components/app/admin/events";
import { formatEventTime } from "@/components/app/admin/event-filters";
import { IPBlockDialog } from "@/components/app/admin/ip-block-dialog";
import {
  AddressLink,
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  MonitoringTable,
  NoneFound,
  monitoringWindow,
} from "@/components/app/admin/monitoring";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Stat } from "@/components/ui/stat";
import { TableCell, TableRow } from "@/components/ui/table";
import { Code, Text, TextLink } from "@/components/ui/text";
import { adminCan, useAdminKey, useAdminSession } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { adminPaths } from "@/lib/api/admin";
import { formatDateTime } from "@/lib/format";
import { useAdminResource } from "@/lib/use-admin-resource";
import { useOpenCount } from "@/lib/use-open-count";
import type { AddressView, MonitoringWindow } from "@/types/admin";
// What did one address (or an IPv6 /64) do? Over the window in the URL:
// when it was first and last seen, its events by name, the accounts seen
// from it, the emails it tried that have no account (how many, never
// which: its events show them), the limits it reached and, for a /64, its
// addresses; and the live IP blocks holding it. One request, recorded with
// the address looked at (admin.ip.viewed): looking an address up is
// following someone.
//
// Operators can block it from here (the IP blocks page's dialog, offering
// an IPv6 address's /64, where one person usually is); the page reloads
// afterwards to show the block, one more recorded read.
export default function AdminMonitoringAddress() {
  const key = useAdminKey();
  const ip = useParams().ip ?? "";
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const window = monitoringWindow(search);
  const resource = useAdminResource(adminPaths.ips.show(ip, window));
  const operator = adminCan(useAdminSession().role, "operator");
  const [blocking, setBlocking] = useState(false);
  const blockOpened = useOpenCount(blocking);
  const setWindow = (w: MonitoringWindow) =>
    void navigate(adminRoutes.monitoring.ip(key, ip, w), {
      preventScrollReset: true,
    });
  return (
    <>
      <MonitoringHeader
        title={ip}
        window={window}
        onWindow={setWindow}
        view={resource.data}
        signals={false}
        actions={
          operator && (
            <Button outline onClick={() => setBlocking(true)}>
              Block this address
            </Button>
          )
        }
      />
      <MonitoringBody resource={resource} label="Loading the address">
        {(view) => <Address view={view} />}
      </MonitoringBody>
      {operator && (
        <IPBlockDialog
          key={blockOpened}
          open={blocking}
          onClose={() => setBlocking(false)}
          onDone={resource.refresh}
          network={resource.data?.network ?? ip}
        />
      )}
    </>
  );
}
function Address({ view }: { view: AddressView }) {
  const key = useAdminKey();
  const window = view.window.name;
  const events = (names?: string[]) =>
    adminRoutes.events.index(key, { ip: view.ip, names, window });
  return (
    <>
      <Blocked view={view} />
      <div className="mt-8 grid gap-8 sm:grid-cols-3">
        <Stat title="Events" value={view.events.toLocaleString("en-US")}>
          <TextLink href={events()}>Its events</TextLink>
        </Stat>
        <Stat
          title="First seen"
          value={view.firstSeen ? formatEventTime(view.firstSeen) : "Not seen"}
        >
          <span className="text-zinc-500 dark:text-zinc-400">
            In this window, UTC
          </span>
        </Stat>
        <Stat
          title="Last seen"
          value={view.lastSeen ? formatEventTime(view.lastSeen) : "Not seen"}
        >
          {view.network ? (
            <span>
              Its /64: <AddressLink ip={view.network} window={window} />
            </span>
          ) : (
            <span className="text-zinc-500 dark:text-zinc-400">
              In this window, UTC
            </span>
          )}
        </Stat>
      </div>
      {view.events === 0 ? (
        <Text className="mt-8">
          Nothing was recorded from this address in this window.
        </Text>
      ) : (
        <>
          <Accounts view={view} />
          <div className="grid gap-x-14 lg:grid-cols-2">
            <ByName view={view} events={events} />
            <Limits view={view} />
          </div>
          {view.ip.includes("/") && <Inside view={view} />}
        </>
      )}
    </>
  );
}
// The live IP blocks holding the address, or part of its /64: its
// requests are refused now.
function Blocked({ view }: { view: AddressView }) {
  const key = useAdminKey();
  if (view.blocks.length === 0) return null;
  return (
    <Notice variant="warning" className="mt-6">
      <span>
        {view.blocks.map((b, i) => (
          <span key={b.network}>
            {i > 0 && "; "}
            <Code>{b.network}</Code> is blocked until{" "}
            {formatDateTime(b.expiresAt)}
          </span>
        ))}
        . Requests from {view.blocks.length === 1 ? "it" : "them"} are refused.{" "}
        <TextLink href={adminRoutes.ipBlocks.index(key)}>IP blocks</TextLink>
      </span>
    </Notice>
  );
}
function Accounts({ view }: { view: AddressView }) {
  return (
    <MonitoringSection
      title="Accounts seen from it"
      description={`Signed in, made there, or failing to sign in. ${unknownEmails(view.unknownEmails)}`}
    >
      {view.accounts.length === 0 ? (
        <NoneFound>No account was seen from it in this window.</NoneFound>
      ) : (
        <MonitoringTable
          head={["Account", "Signed in", "Made here", "Failed sign-ins"]}
        >
          {view.accounts.map((a) => (
            <TableRow key={a.account.id}>
              <TableCell>
                <AccountLink account={a.account} />
              </TableCell>
              <TableCell className="tabular-nums">{a.signedIn}</TableCell>
              <TableCell className="tabular-nums">{a.registered}</TableCell>
              <TableCell className="tabular-nums">{a.failed}</TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
function ByName({
  view,
  events,
}: {
  view: AddressView;
  events: (names?: string[]) => string;
}) {
  return (
    <MonitoringSection title="Events by name">
      <MonitoringTable head={["Event", "Count"]}>
        {view.byName.map((n) => (
          <TableRow key={n.name} href={events([n.name])}>
            <TableCell>
              <Code>{n.name}</Code>
            </TableCell>
            <TableCell className="tabular-nums">{n.count}</TableCell>
          </TableRow>
        ))}
      </MonitoringTable>
    </MonitoringSection>
  );
}
function Limits({ view }: { view: AddressView }) {
  return (
    <MonitoringSection title="Limits reached">
      {view.limits.length === 0 ? (
        <NoneFound>It reached no limit in this window.</NoneFound>
      ) : (
        <MonitoringTable head={["Limit", "Windows", "Last (UTC)"]}>
          {view.limits.map((l) => (
            <TableRow key={l.limit}>
              <TableCell>
                <Code>{l.limit}</Code>
              </TableCell>
              <TableCell className="tabular-nums">{l.windows}</TableCell>
              <TableCell>{formatEventTime(l.lastReached)}</TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
// A /64's addresses, busiest first.
function Inside({ view }: { view: AddressView }) {
  return (
    <MonitoringSection title="Addresses in this /64">
      <MonitoringTable head={["Address", "Events"]}>
        {view.addresses.map((a) => (
          <TableRow key={a.ip}>
            <TableCell>
              <AddressLink ip={a.ip} window={view.window.name} />
            </TableCell>
            <TableCell className="tabular-nums">{a.count}</TableCell>
          </TableRow>
        ))}
      </MonitoringTable>
    </MonitoringSection>
  );
}
// The emails tried that have no account: how many, never which.
function unknownEmails(n: number): string {
  if (n === 0) return "It tried no email without an account.";
  return `It tried ${n.toLocaleString("en-US")} ${n === 1 ? "email" : "different emails"} with no account too: its events show which.`;
}
