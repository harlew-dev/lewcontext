import {
  BuildingOffice2Icon,
  EnvelopeIcon,
  QueueListIcon,
  UsersIcon,
} from "@heroicons/react/20/solid";
import type { DeliveryPageQuery, EventFilters } from "@/types/admin";
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
      url(`/admin/${key}/events`, filters),
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
} as const;
// Sidebar items for the admin dashboard (Monitoring comes with its plan).
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
  ];
}
