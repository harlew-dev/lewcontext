import { useNavigate, useSearchParams } from "react-router";
import {
  AdminBadge,
  AdminTableHead,
  MfaBadge,
  adminListPath,
  adminListQuery,
  type AdminColumn,
} from "@/components/app/admin/admin-list";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { PaginatedTable } from "@/components/ui/paginated-table";
import { SearchField } from "@/components/ui/search-field";
import { TableCell, TableRow } from "@/components/ui/table";
import { paths } from "@/lib/api";
import { useAdminKey } from "@/lib/admin";
import { formatDate } from "@/lib/format";
import { routes } from "@/lib/routes";
import { useAdminResource } from "@/lib/use-admin-resource";
import {
  adminUserSortable,
  defaultAdminUserSort,
  type AdminListQuery,
  type AdminUser,
  type AdminUserSort,
} from "@/types/admin";
const columns: AdminColumn<(typeof adminUserSortable)[number]>[] = [
  { label: "Name", field: "name", first: "ascending" },
  {
    label: "Email",
    field: "email",
    first: "ascending",
    className: "max-md:hidden",
  },
  { label: "Workspaces", className: "max-sm:hidden" },
  { label: "Last sign-in", className: "max-lg:hidden" },
  {
    label: "Joined",
    field: "created",
    first: "descending",
    className: "max-sm:hidden",
  },
];
// Every account, newest first: search by name or email, sort by a column.
// The state lives in the URL; each page loaded is recorded in the audit trail.
export default function AdminUserIndex() {
  const key = useAdminKey();
  const [search] = useSearchParams();
  const goTo = useNavigate();
  const query = adminListQuery(search, adminUserSortable, defaultAdminUserSort);
  const list = useAdminResource(paths.admin.users.index(query));
  const pathFor = (q: AdminListQuery<AdminUserSort>) =>
    adminListPath(routes.admin.users.index(key), q, defaultAdminUserSort);
  return (
    <>
      <Heading>Users</Heading>
      <div className="mt-4 max-w-md">
        <SearchField
          label="Search users"
          value={query.q}
          onSearch={(q) =>
            void goTo(pathFor({ ...query, page: 1, q }), {
              replace: true,
              preventScrollReset: true,
            })
          }
        />
      </div>
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      <PaginatedTable
        className="mt-10"
        page={list.data}
        loading={list.loading}
        label="Users"
        head={
          <AdminTableHead
            columns={columns}
            sort={query.sort}
            sortHref={(sort) => pathFor({ ...query, page: 1, sort })}
          />
        }
        row={(user) => <UserRow user={user} />}
        rowKey={(user) => user.id}
        href={(page) => pathFor({ ...query, page })}
        empty={
          <EmptyState
            title={query.q ? "No users match" : "No users yet"}
            description={
              query.q
                ? "Try another name or email address."
                : "Accounts appear here once people sign up."
            }
          />
        }
      />
    </>
  );
}
function UserRow({ user }: { user: AdminUser }) {
  const key = useAdminKey();
  return (
    <TableRow
      href={routes.admin.users.show(key, user.id)}
      title={`Open ${user.name}`}
    >
      <TableCell>
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{user.name}</span>
          <MfaBadge enabled={user.mfaEnabled} />
          {user.admin && <AdminBadge />}
        </div>
      </TableCell>
      <TableCell className="text-zinc-500 max-md:hidden">
        {user.email}
      </TableCell>
      <TableCell className="max-sm:hidden">{user.workspaceCount}</TableCell>
      <TableCell className="text-zinc-500 max-lg:hidden">
        {user.lastSignedInAt ? formatDate(user.lastSignedInAt) : "Never"}
      </TableCell>
      <TableCell className="text-zinc-500 max-sm:hidden">
        {formatDate(user.createdAt)}
      </TableCell>
    </TableRow>
  );
}
