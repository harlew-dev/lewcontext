import { Badge, LevelBadge } from "@/components/ui/badge";
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
import { formatEventTime } from "@/components/app/admin/event-filters";
import { TableCell, TableRow } from "@/components/ui/table";
import { Code, Text } from "@/components/ui/text";
import { adminRoutes } from "@/lib/admin-routes";
import { cx } from "@/lib/cx";
import type { ConfigurationSections, MonitoringView } from "@/types/admin";
// Which servers and versions are live, and is anything set unsafely? From
// each server's summary of its configuration, which names the settings
// that are set and their values, never a secret's value or digest. The
// live servers' settings side by side, differences marked, then warnings
// with what to do. One request, one recorded read.
export default function AdminMonitoringConfiguration() {
  const page = useMonitoringPage<ConfigurationSections>(
    "configuration",
    adminRoutes.monitoring.configuration,
  );
  return (
    <>
      <MonitoringHeader
        title="Configuration"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading the configuration">
        {(view) => <Configuration view={view} />}
      </MonitoringBody>
    </>
  );
}
type View = MonitoringView<ConfigurationSections>;
function Configuration({ view }: { view: View }) {
  return (
    <>
      <SignalGrid className="mt-8" signals={view.signals} />
      <SectionGate names={["servers", "warnings"]} title="Warnings">
        <Warnings view={view} />
      </SectionGate>
      <SectionGate names={["servers"]} title="Servers">
        <Servers view={view} />
      </SectionGate>
      <SectionGate names={["servers"]} title="Settings">
        <Settings view={view} />
      </SectionGate>
    </>
  );
}
// Each warning by its ID: what it means and what to do, with its count.
const warningText: Record<
  string,
  (count: number) => { title: string; action: string }
> = {
  not_production: (n) => ({
    title: `${servers(n)} not running as production (APP_ENV).`,
    action:
      "Fine in development. In production, set APP_ENV=production: it turns on HTTPS-only cookies and the production checks.",
  }),
  mail_off: (n) => ({
    title: `MAIL_DRIVER=off on ${servers(n)}: no email is sent.`,
    action:
      "Reset links and invitations never arrive. Set MAIL_DRIVER=smtp with the SMTP settings.",
  }),
  allowlist_without_client_ip: (n) => ({
    title: `ADMIN_ALLOWED_IPS without CLIENT_IP_HEADER on ${servers(n)}.`,
    action:
      "Behind a proxy every request comes from the proxy, so the allowlist checks the proxy's address. Set CLIENT_IP_HEADER and TRUSTED_PROXIES, or remove the allowlist.",
  }),
  previous_key: (n) => ({
    title: `ADMIN_ROUTE_KEY_PREVIOUS is still set; ${n.toLocaleString("en-US")} admin ${n === 1 ? "session uses" : "sessions use"} it.`,
    action:
      "Hand out the new address, then remove the previous key and restart: those sessions end (at most 8 hours after they began anyway).",
  }),
  metrics_without_token: (n) => ({
    title: `METRICS_ADDR without METRICS_TOKEN on ${servers(n)}.`,
    action:
      "Production refuses this on a public address; on a private one only the firewall keeps /metrics in. Set METRICS_TOKEN.",
  }),
  captcha_off: (n) => ({
    title: `CAPTCHA_PROVIDER=off in production on ${servers(n)}.`,
    action:
      "Rate limits' challenge levels are skipped, so scripted sign-ins and sign-ups meet only the block levels. Set CAPTCHA_PROVIDER=turnstile with your own widget's keys.",
  }),
  migrations_pending: (n) => ({
    title: `${servers(n)} built with migrations the database hasn't applied.`,
    action: "Run the migrations (make migrate), then restart the server.",
  }),
  versions: (n) => ({
    title: `${n} versions are live.`,
    action:
      "A release in progress, or one stopped halfway: finish rolling it out, or roll it back.",
  }),
  no_admin_with_mfa: () => ({
    title: "No admin has two-factor on.",
    action:
      "Nobody can sign in to this dashboard. Grant admin from the command line (cmd/admin grant): it opens the window to set it up.",
  }),
  shared_address: (n) => ({
    title: `One address sent ${Math.round(n)}% of the last day's events.`,
    action:
      "The server probably isn't reading the client's address behind its proxy: set CLIENT_IP_HEADER and TRUSTED_PROXIES. Every number by address is the proxy's until then.",
  }),
};
function servers(n: number): string {
  return n === 1 ? "1 server" : `${n.toLocaleString("en-US")} servers`;
}
function Warnings({ view }: { view: View }) {
  const warnings = view.sections.warnings;
  return (
    <MonitoringSection
      title="Warnings"
      description="Settings that are unsafe or broken on the servers compared, each with what to do. Prune not running is the Data page's."
    >
      {warnings.length === 0 ? (
        <NoneFound>Nothing set unsafely.</NoneFound>
      ) : (
        <ul className="divide-y divide-zinc-950/5 dark:divide-white/5">
          {warnings.map((w) => {
            const text = warningText[w.id]?.(w.count ?? 0);
            if (!text) return null;
            return (
              <li key={w.id} className="flex gap-4 py-4 first:pt-0">
                <LevelBadge level="warn" />
                <div className="text-sm/6">
                  <div className="font-medium text-zinc-950 dark:text-white">
                    {text.title}
                  </div>
                  <div className="text-zinc-500 dark:text-zinc-400">
                    {text.action}
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </MonitoringSection>
  );
}
function Servers({ view }: { view: View }) {
  const list = view.sections.servers;
  return (
    <MonitoringSection
      title="Servers"
      description="Every server heard from in the last day. Live: heard from in the last 2 minutes; a stopped server's row goes after a day."
    >
      {list.length === 0 ? (
        <NoneFound>
          No server has written a heartbeat in the last day.
        </NoneFound>
      ) : (
        <MonitoringTable
          head={["Server", "Version", "Started", "Last heard from"]}
        >
          {list.map((s) => (
            <TableRow key={s.id}>
              <TableCell>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{s.hostname}</span>
                  <Badge color="zinc">{s.live ? "Live" : "Silent"}</Badge>
                </div>
                <Code>{s.id}</Code>
              </TableCell>
              <TableCell>
                <Code>{s.version}</Code>
              </TableCell>
              <TableCell>{formatEventTime(s.startedAt)}</TableCell>
              <TableCell>{formatEventTime(s.lastSeenAt)}</TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
    </MonitoringSection>
  );
}
function Settings({ view }: { view: View }) {
  const { settings, compared, servers: list } = view.sections;
  const names = compared.map(
    (id) => list.find((s) => s.id === id)?.hostname ?? id,
  );
  return (
    <MonitoringSection
      title="Settings"
      description="Each live server's settings side by side (the last heard from when none is live). Secrets show only whether they're set. A setting that differs between servers is marked."
    >
      {settings.length === 0 ? (
        <NoneFound>No server to compare.</NoneFound>
      ) : (
        <>
          <MonitoringTable head={["Setting", ...names]}>
            {settings.map((s) => (
              <TableRow key={s.name}>
                <TableCell>
                  <span className="flex flex-wrap items-center gap-2">
                    <Code>{s.name}</Code>
                    {s.differs && <Badge color="zinc">Differs</Badge>}
                  </span>
                </TableCell>
                {s.values.map((v, i) => (
                  <TableCell
                    key={compared[i]}
                    className={cx(
                      s.differs && "font-medium text-zinc-950 dark:text-white",
                    )}
                  >
                    {v || "–"}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </MonitoringTable>
          <Text className="mt-3">
            Each server writes its summary when it starts.
          </Text>
        </>
      )}
    </MonitoringSection>
  );
}
