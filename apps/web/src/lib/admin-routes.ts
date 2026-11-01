import {
  BuildingOffice2Icon,
  ChartBarIcon,
  GlobeAltIcon,
  RectangleStackIcon,
  UserGroupIcon,
  EnvelopeIcon,
  KeyIcon,
  NoSymbolIcon,
  QueueListIcon,
  ShieldExclamationIcon,
  Squares2X2Icon,
  UsersIcon,
  ClockIcon,
} from "@heroicons/react/20/solid";
import {
  defaultEventWindow,
  defaultMonitoringWindow,
  type DeliveryPageQuery,
  type EventFilters,
  type MonitoringWindow,
} from "@/types/admin";
import { url } from "./pagination";
// The admin dashboard's page URLs, at its secret key (ADMIN_ROUTE_KEY, from
// the URL; lib/admin.tsx), registered with their page files in src/routes.ts.
// Only admin code imports this file, so none of it reaches the app build
// (plans/features/admin/README.md, rule 7). Never a ?next=: signing in always
// lands on Users.
export const adminRoutes = {
  login: (key: string) => `/admin/${key}`,
  users: {
    index: (key: string) => `/admin/${key}/users`,
    show: (key: string, user: string) => `/admin/${key}/users/${user}`,
  },
  workspaces: {
    index: (key: string) => `/admin/${key}/workspaces`,
    show: (key: string, workspace: string) =>
      `/admin/${key}/workspaces/${workspace}`,
  },
  // Events, filtered: links from elsewhere open the rows behind them.
  events: {
    index: (key: string, filters: EventFilters = {}) =>
      eventsUrl(`/admin/${key}/events`, eventQuery(filters)),
    show: (key: string, event: string) => `/admin/${key}/events/${event}`,
  },
  // The Mail page lists deliveries (AdminDeliveryIndex), with the summary's
  // window (24h is the default), a status tab and a cursor.
  deliveries: {
    index: (key: string, { window, status, before }: DeliveryPageQuery = {}) =>
      url(`/admin/${key}/mail`, {
        window: window === "24h" ? undefined : window,
        status,
        before,
      }),
  },
  // The block list: addresses whose owners asked for no email.
  emailBlocks: {
    index: (key: string) => `/admin/${key}/email-blocks`,
  },
  // Who holds admin: roles, MFA, last sign-in, live sessions.
  admins: {
    index: (key: string) => `/admin/${key}/admins`,
  },
  // The Monitoring pages, each with its window (24h is the default).
  monitoring: {
    index: (key: string, window?: MonitoringWindow) =>
      monitoringUrl(`/admin/${key}/monitoring`, window),
    security: (key: string, window?: MonitoringWindow) =>
      monitoringUrl(`/admin/${key}/monitoring/security`, window),
    limits: (key: string, window?: MonitoringWindow) =>
      monitoringUrl(`/admin/${key}/monitoring/limits`, window),
    ips: (key: string, window?: MonitoringWindow) =>
      monitoringUrl(`/admin/${key}/monitoring/ips`, window),
    // One address, or an IPv6 /64 (its slash escaped in the path).
    ip: (key: string, ip: string, window?: MonitoringWindow) =>
      monitoringUrl(
        `/admin/${key}/monitoring/ips/${addressSegment(ip)}`,
        window,
      ),
    accounts: (key: string, window?: MonitoringWindow) =>
      monitoringUrl(`/admin/${key}/monitoring/accounts`, window),
    workspaces: (key: string, window?: MonitoringWindow) =>
      monitoringUrl(`/admin/${key}/monitoring/workspaces`, window),
    usage: (key: string, window?: MonitoringWindow) =>
      monitoringUrl(`/admin/${key}/monitoring/usage`, window),
  },
} as const;
// An address as one path segment: an IPv6 /64's slash escaped, its colons
// kept as they are (a path segment may hold them).
export function addressSegment(ip: string): string {
  return encodeURIComponent(ip).replaceAll("%3A", ":");
}
// A Monitoring page's URL, the default window left out.
function monitoringUrl(path: string, window?: MonitoringWindow): string {
  return url(path, {
    window: window === defaultMonitoringWindow ? undefined : window,
  });
}
// The Monitoring heading's items in the sidebar: the pages built so far
// (plans/features/admin/monitoring.md ships them one by one).
export function adminMonitoringNavigation(key: string) {
  return [
    {
      label: "Overview",
      href: adminRoutes.monitoring.index(key),
      icon: Squares2X2Icon,
    },
    {
      label: "Security",
      href: adminRoutes.monitoring.security(key),
      icon: ShieldExclamationIcon,
    },
    {
      label: "Rate limits",
      href: adminRoutes.monitoring.limits(key),
      icon: ClockIcon,
    },
    {
      label: "IP addresses",
      href: adminRoutes.monitoring.ips(key),
      icon: GlobeAltIcon,
    },
    {
      label: "Accounts",
      href: adminRoutes.monitoring.accounts(key),
      icon: UserGroupIcon,
    },
    {
      label: "Workspaces",
      href: adminRoutes.monitoring.workspaces(key),
      icon: RectangleStackIcon,
    },
    {
      label: "Usage",
      href: adminRoutes.monitoring.usage(key),
      icon: ChartBarIcon,
    },
  ];
}
// Sidebar items for the admin dashboard; Monitoring's are under their own
// heading (adminMonitoringNavigation).
export function adminNavigation(key: string) {
  return [
    { label: "Users", href: adminRoutes.users.index(key), icon: UsersIcon },
    {
      label: "Workspaces",
      href: adminRoutes.workspaces.index(key),
      icon: BuildingOffice2Icon,
    },
    {
      label: "Events",
      href: adminRoutes.events.index(key),
      icon: QueueListIcon,
    },
    {
      label: "Mail",
      href: adminRoutes.deliveries.index(key),
      icon: EnvelopeIcon,
    },
    {
      label: "Email blocks",
      href: adminRoutes.emailBlocks.index(key),
      icon: NoSymbolIcon,
    },
    { label: "Admins", href: adminRoutes.admins.index(key), icon: KeyIcon },
  ];
}
// The Events filters as query parameters, for the page URL and the API
// alike: the name tokens as one comma-separated `name`, the default window
// left out, so each view has one URL and one SWR key.
export function eventQuery(filters: EventFilters) {
  return {
    window: filters.window === defaultEventWindow ? undefined : filters.window,
    name: filters.names?.length ? filters.names.join(",") : undefined,
    outcome: filters.outcome,
    account: filters.account,
    workspace: filters.workspace,
    email: filters.email,
    ip: filters.ip,
    from: filters.from,
    to: filters.to,
    before: filters.before,
  };
}
// `url`, keeping the name list's commas readable ("auth.*,project.*").
export function eventsUrl(
  path: string,
  params: Parameters<typeof url>[1],
): string {
  return url(path, params).replaceAll("%2C", ",");
}
