import { Badge, type BadgeColor } from "@/components/ui/badge";
import { TableHeader, TableRow } from "@/components/ui/table";
import {
  cleanSearch,
  page,
  sort,
  sortDirection,
  toggleSort,
  url,
  type Sort,
  type SortDirection,
} from "@/lib/pagination";
import { formatDateTime } from "@/lib/format";
import type {
  AdminDelivery,
  AdminListQuery,
  DeliveryStatus,
} from "@/types/admin";
// Shared by the admin dashboard's Users, Workspaces and Email blocks lists: their URL
// state (?q=&sort=&page=), a sortable header row, and the account badges;
// and by Events and Mail: a delivery's status badge.

// A list page's state in its URL. Values the API would reject fall back to
// defaults, so a hand-edited URL still renders a page.
export function adminListQuery<F extends string>(
  query: URLSearchParams,
  fields: readonly F[],
  fallback: Sort<F>,
): AdminListQuery<Sort<F>> {
  return {
    page: page(query),
    q: cleanSearch(query.get("q") ?? ""),
    sort: sort(query, fields, fallback),
  };
}
// The list page's URL for a query, leaving out defaults.
export function adminListPath<S extends string>(
  path: string,
  { page, q, sort }: AdminListQuery<S>,
  fallback: S,
): string {
  return url(path, { q, sort: sort === fallback ? "" : sort, page });
}
export type AdminColumn<F extends string> = {
  label: string;
  // Sortable columns name their field; dates sort newest first.
  field?: F;
  first?: SortDirection;
  className?: string;
  // Named for screen readers only (the ⋯ menus' column).
  hidden?: boolean;
};
// A header row whose sortable columns link to their sort.
export function AdminTableHead<F extends string>({
  columns,
  sort,
  sortHref,
}: {
  columns: AdminColumn<F>[];
  sort: Sort<F>;
  sortHref: (sort: Sort<F>) => string;
}) {
  return (
    <TableRow>
      {columns.map(({ label, field, first, className, hidden }) => (
        <TableHeader
          key={label}
          className={className}
          href={field ? sortHref(toggleSort(sort, field, first)) : undefined}
          sort={field ? sortDirection(sort, field) : undefined}
        >
          {hidden ? <span className="sr-only">{label}</span> : label}
        </TableHeader>
      ))}
    </TableRow>
  );
}
// Whether the account has two-factor authentication on: green when it does.
export function MfaBadge({ enabled }: { enabled: boolean }) {
  return enabled ? <Badge color="lime">MFA</Badge> : <Badge>No MFA</Badge>;
}
// Whether the account proved it owns its email. Lists show only "Not
// verified", so most rows stay quiet; a user's page shows either.
export function EmailBadge({
  verifiedAt,
  quiet = false,
}: {
  verifiedAt: string | null;
  quiet?: boolean;
}) {
  if (verifiedAt) return quiet ? null : <Badge color="lime">Verified</Badge>;
  return <Badge>Not verified</Badge>;
}
export function AdminBadge() {
  return <Badge color="purple">Admin</Badge>;
}
// An account an admin suspended: it can't sign in.
export function SuspendedBadge() {
  return <Badge color="red">Suspended</Badge>;
}
// A delivery's status, the word always beside the colour.
const deliveryBadges: Record<
  DeliveryStatus,
  { label: string; color: BadgeColor }
> = {
  delivered: { label: "Delivered", color: "lime" },
  pending: { label: "Pending", color: "zinc" },
  retrying: { label: "Retrying", color: "pink" },
  failed: { label: "Failed", color: "rose" },
};
export function DeliveryStatusBadge({ status }: { status: DeliveryStatus }) {
  const { label, color } = deliveryBadges[status];
  return <Badge color={color}>{label}</Badge>;
}
// When a delivery finished, or when it's tried next.
export function DeliveryTime({ delivery: d }: { delivery: AdminDelivery }) {
  if (d.deliveredAt) return <>{formatDateTime(d.deliveredAt)}</>;
  if (d.failedAt) return <>Gave up {formatDateTime(d.failedAt)}</>;
  if (d.nextAttemptAt) return <>Next try {formatDateTime(d.nextAttemptAt)}</>;
  return <>–</>;
}
