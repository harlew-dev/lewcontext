import { useNavigate, useSearchParams } from "react-router";
import {
  AdminTableHead,
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
  adminWorkspaceSortable,
  defaultAdminWorkspaceSort,
  type AdminListQuery,
  type AdminWorkspace,
  type AdminWorkspaceSort,
} from "@/types/admin";
const columns: AdminColumn<(typeof adminWorkspaceSortable)[number]>[] = [
  { label: "Name", field: "name", first: "ascending" },
  { label: "Owners", className: "max-md:hidden" },
  { label: "Members", className: "max-sm:hidden" },
  { label: "Projects", className: "max-sm:hidden" },
  {
    label: "Created",
    field: "created",
    first: "descending",
    className: "max-lg:hidden",
  },
];
// Every workspace, newest first, with its counts and first owners: search by
// name, sort by a column. Project names and contents aren't shown.
export default function AdminWorkspaceIndex() {
  const key = useAdminKey();
  const [search] = useSearchParams();
  const goTo = useNavigate();
  const query = adminListQuery(
    search,
    adminWorkspaceSortable,
    defaultAdminWorkspaceSort,
  );
  const list = useAdminResource(paths.admin.workspaces.index(query));
  const pathFor = (q: AdminListQuery<AdminWorkspaceSort>) =>
    adminListPath(
      routes.admin.workspaces.index(key),
      q,
      defaultAdminWorkspaceSort,
    );
  return (
    <>
      <Heading>Workspaces</Heading>
      <div className="mt-4 max-w-md">
        <SearchField
          label="Search workspaces"
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
        label="Workspaces"
        head={
          <AdminTableHead
            columns={columns}
            sort={query.sort}
            sortHref={(sort) => pathFor({ ...query, page: 1, sort })}
          />
        }
        row={(workspace) => <WorkspaceRow workspace={workspace} />}
        rowKey={(workspace) => workspace.id}
        href={(page) => pathFor({ ...query, page })}
        empty={
          <EmptyState
            title={query.q ? "No workspaces match" : "No workspaces yet"}
            description={
              query.q
                ? "Try another name."
                : "Workspaces appear here once people create them."
            }
          />
        }
      />
    </>
  );
}
function WorkspaceRow({ workspace }: { workspace: AdminWorkspace }) {
  const key = useAdminKey();
  return (
    <TableRow
      href={routes.admin.workspaces.show(key, workspace.id)}
      title={`Open ${workspace.name}`}
    >
      <TableCell className="font-medium">{workspace.name}</TableCell>
      <TableCell className="text-zinc-500 max-md:hidden">
        {workspace.owners.map((owner) => owner.name).join(", ") || "None"}
      </TableCell>
      <TableCell className="max-sm:hidden">{workspace.memberCount}</TableCell>
      <TableCell className="max-sm:hidden">{workspace.projectCount}</TableCell>
      <TableCell className="text-zinc-500 max-lg:hidden">
        {formatDate(workspace.createdAt)}
      </TableCell>
    </TableRow>
  );
}
