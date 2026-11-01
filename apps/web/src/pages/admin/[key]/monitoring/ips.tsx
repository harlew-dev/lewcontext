import { useNavigate, useSearchParams } from "react-router";
import {
  AddressLink,
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  MonitoringTable,
  NoneFound,
  SectionGate,
  SignalGrid,
  useMonitoringPage,
} from "@/components/app/admin/monitoring";
import { formatClock, formatDay } from "@/components/app/admin/event-filters";
import { Notice } from "@/components/ui/feedback";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { url } from "@/lib/pagination";
import type { IpsSections, MonitoringView } from "@/types/admin";
// Which addresses are noisy or abusive? The addresses events came from,
// sorted by any column, IPv6 by its /64 or each address on its own; the
// addresses that made many accounts; and a warning when one address holds
// most of the traffic (the proxy's client IP probably isn't set, so every
// number here is wrong). Each address opens its own page. One request, one
// recorded read; sorting and the IPv6 switch only rearrange what came.
//
// TODO(A6, actions): "Block this address" (POST /api/admin/ip-blocks, an
// AdminActionDialog for operators) plugs in on each address's page,
// ips/[ip].tsx, once the actions branch is merged.
export default function AdminMonitoringIPs() {
  const page = useMonitoringPage<IpsSections>(
    "ips",
    adminRoutes.monitoring.ips,
  );
  return (
    <>
      <MonitoringHeader
        title="IP addresses"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading IP addresses">
        {(view) => <Addresses view={view} />}
      </MonitoringBody>
    </>
  );
}
type View = MonitoringView<IpsSections>;
function Addresses({ view }: { view: View }) {
  return (
    <>
      <SharedAddress view={view} />
      <SignalGrid className="mt-8" signals={view.signals} />
      <SectionGate names={["addresses"]} title="Top addresses">
        <TopAddresses view={view} />
      </SectionGate>
      <SectionGate names={["clusters"]} title="Sign-up clusters">
        <Clusters view={view} />
      </SectionGate>
    </>
  );
}
// One address holding most of the traffic: the numbers by address are
// then the proxy's.
function SharedAddress({ view }: { view: View }) {
  const busiest = view.signals.find((s) => s.name === "ips.busiest_share");
  if (!busiest || busiest.unavailable || busiest.level === "ok") return null;
  return (
    <Notice variant="warning" className="mt-6">
      One address sent {Math.round(busiest.value)}% of the last day&rsquo;s
      events. The server probably isn&rsquo;t reading the client&rsquo;s address
      behind its proxy (CLIENT_IP_HEADER, TRUSTED_PROXIES), so every number by
      address on this page is the proxy&rsquo;s.
    </Notice>
  );
}
// The columns a top list sorts by (?sort=, events by default), largest
// first.
const columns = [
  { key: "events", label: "Events" },
  { key: "failedSignIns", label: "Failed sign-ins" },
  { key: "signUps", label: "Sign-ups" },
  { key: "limitsReached", label: "Limits reached" },
  { key: "accounts", label: "Accounts" },
] as const;
type SortKey = (typeof columns)[number]["key"];
// The server keeps the top 10 by each column, so the first 10 by any one
// are its true top 10.
const shown = 10;
// The list's state in the URL: its sort, and IPv6 each on its own.
function useTopState(view: View) {
  const key = useAdminKey();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const sort: SortKey =
    columns.find((c) => c.key === search.get("sort"))?.key ?? "events";
  const each = search.get("ipv6") === "each";
  const href = (next: { sort: SortKey; each: boolean }) =>
    url(adminRoutes.monitoring.ips(key, view.window.name), {
      sort: next.sort === "events" ? undefined : next.sort,
      ipv6: next.each ? "each" : undefined,
    });
  const setEach = (value: boolean) =>
    void navigate(href({ sort, each: value }), { preventScrollReset: true });
  return { sort, each, href, setEach };
}
function TopAddresses({ view }: { view: View }) {
  const { sort, each, href, setEach } = useTopState(view);
  const rows = view.sections.addresses
    .filter((r) => r.network !== each)
    .sort((a, b) => b[sort] - a[sort] || a.ip.localeCompare(b.ip))
    .slice(0, shown);
  return (
    <MonitoringSection
      title="Top addresses"
      description="Where events came from, the dashboard's own reads left out. The ten highest by the column sorted on."
    >
      <div className="mb-4 max-w-xs">
        <Listbox
          aria-label="IPv6 addresses"
          value={each ? "each" : "network"}
          onChange={(v: string) => setEach(v === "each")}
        >
          <ListboxOption value="network">
            <ListboxLabel>IPv6 by its /64</ListboxLabel>
          </ListboxOption>
          <ListboxOption value="each">
            <ListboxLabel>IPv6 address by address</ListboxLabel>
          </ListboxOption>
        </Listbox>
      </div>
      {rows.length === 0 ? (
        <NoneFound>No event came from an address in this window.</NoneFound>
      ) : (
        <Table dense>
          <TableHead>
            <TableRow>
              <TableHeader>Address</TableHeader>
              {columns.map((c) => (
                <TableHeader
                  key={c.key}
                  href={href({ sort: c.key, each })}
                  sort={c.key === sort ? "descending" : undefined}
                >
                  {c.label}
                </TableHeader>
              ))}
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((r) => (
              <TableRow key={r.ip}>
                <TableCell>
                  <AddressLink ip={r.ip} window={view.window.name} />
                </TableCell>
                {columns.map((c) => (
                  <TableCell key={c.key} className="tabular-nums">
                    {r[c.key].toLocaleString("en-US")}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </MonitoringSection>
  );
}
function Clusters({ view }: { view: View }) {
  const rows = view.sections.clusters;
  return (
    <MonitoringSection
      title="Sign-up clusters"
      description="Addresses (an IPv6 address by its /64) that made 5 or more accounts in a day: the usual shape of a spam farm. 20 is critical."
    >
      {rows.length === 0 ? (
        <NoneFound>No address made 5 or more accounts in a day.</NoneFound>
      ) : (
        <MonitoringTable head={["Day from (UTC)", "Address", "Accounts"]}>
          {rows.map((r) => (
            <TableRow key={`${r.period}${r.ip}`}>
              <TableCell>
                {formatDay(new Date(r.period))},{" "}
                {formatClock(new Date(r.period))}
              </TableCell>
              <TableCell>
                <AddressLink ip={r.ip} window={view.window.name} />
              </TableCell>
              <TableCell className="tabular-nums">{r.accounts}</TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
