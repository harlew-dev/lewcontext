import { Badge } from "@/components/ui/badge";
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
import type { AdminListQuery } from "@/types/admin";
// Shared by the admin dashboard's Users and Workspaces lists: their URL
// state (?q=&sort=&page=), a sortable header row, and the account badges.

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
      {columns.map(({ label, field, first, className }) => (
        <TableHeader
          key={label}
          className={className}
          href={field ? sortHref(toggleSort(sort, field, first)) : undefined}
          sort={field ? sortDirection(sort, field) : undefined}
        >
          {label}
        </TableHeader>
      ))}
    </TableRow>
  );
}
// Whether the account has two-factor authentication on: green when it does.
export function MfaBadge({ enabled }: { enabled: boolean }) {
  return enabled ? <Badge color="lime">MFA</Badge> : <Badge>No MFA</Badge>;
}
export function AdminBadge() {
  return <Badge color="purple">Admin</Badge>;
}
