import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { EllipsisHorizontalIcon } from "@heroicons/react/16/solid";
import { AdminActionDialog } from "@/components/app/admin/admin-action-dialog";
import {
  AdminTableHead,
  adminListPath,
  adminListQuery,
  type AdminColumn,
} from "@/components/app/admin/admin-list";
import { Badge } from "@/components/ui/badge";
import {
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { PaginatedTable } from "@/components/ui/paginated-table";
import { SearchField } from "@/components/ui/search-field";
import { TableCell, TableRow } from "@/components/ui/table";
import { Text } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { ApiError } from "@/lib/api";
import { adminPaths, useAdminApi } from "@/lib/api/admin";
import { adminCan, useAdminKey, useAdminSession } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { appName } from "@/lib/app";
import { formatDate } from "@/lib/format";
import { useAdminResource } from "@/lib/use-admin-resource";
import { useOpenCount } from "@/lib/use-open-count";
import {
  adminEmailBlockSortable,
  defaultAdminEmailBlockSort,
  type AdminEmailBlock,
  type AdminEmailBlockSort,
  type AdminListQuery,
} from "@/types/admin";
const columns: AdminColumn<(typeof adminEmailBlockSortable)[number]>[] = [
  { label: "Address", field: "email", first: "ascending" },
  {
    label: "Blocked",
    field: "created",
    first: "descending",
    className: "max-sm:hidden",
  },
  { label: "Owner’s link", className: "max-md:hidden" },
  { label: "Actions", className: "w-0", hidden: true },
];
// The block list (plans/features/implemented/admin/email-blocks.md): every
// address whose owner asked for no email, newest first, with a search and
// sorts in the URL. Each page loaded is recorded in the audit trail. A row's
// ⋯ menu opens its activity in Events, and for operators unblocks it,
// behind a reason and a fresh code. Blocking is only ever the owner's.
export default function AdminEmailBlockIndex() {
  const key = useAdminKey();
  const [search] = useSearchParams();
  const goTo = useNavigate();
  const query = adminListQuery(
    search,
    adminEmailBlockSortable,
    defaultAdminEmailBlockSort,
  );
  const list = useAdminResource(adminPaths.emailBlocks.index(query));
  const pathFor = (q: AdminListQuery<AdminEmailBlockSort>) =>
    adminListPath(
      adminRoutes.emailBlocks.index(key),
      q,
      defaultAdminEmailBlockSort,
    );
  const [unblocking, setUnblocking] = useState<AdminEmailBlock | null>(null);
  const opened = useOpenCount(unblocking !== null);
  // The last one stays rendered while its dialog animates out.
  const [shown, setShown] = useState<AdminEmailBlock | null>(null);
  if (unblocking && unblocking !== shown) setShown(unblocking);
  return (
    <>
      <Heading>Email blocks</Heading>
      <Text className="mt-2 max-w-2xl">
        Addresses whose owners asked for no email from {appName}, from a link in
        one of our emails. A blocked address gets no email and can’t be used to
        sign up.
      </Text>
      <div className="mt-4 max-w-md">
        <SearchField
          label="Search addresses"
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
        label="Email blocks"
        head={
          <AdminTableHead
            columns={columns}
            sort={query.sort}
            sortHref={(sort) => pathFor({ ...query, page: 1, sort })}
          />
        }
        row={(block) => <BlockRow block={block} onUnblock={setUnblocking} />}
        rowKey={(block) => block.email}
        href={(page) => pathFor({ ...query, page })}
        empty={
          <EmptyState
            title={
              query.q
                ? `No addresses match “${query.q}”`
                : "No blocked addresses"
            }
            description={
              query.q
                ? "Try another part of the address."
                : "An address appears here when its owner blocks it from the link in one of our emails."
            }
          />
        }
      />
      {shown && (
        <UnblockDialog
          key={opened}
          open={unblocking !== null}
          block={shown}
          onClose={() => setUnblocking(null)}
          onDone={() => {
            toast.success(`${shown.email} unblocked.`);
            setUnblocking(null);
            list.refresh();
          }}
          onGone={list.refresh}
        />
      )}
    </>
  );
}
function BlockRow({
  block,
  onUnblock,
}: {
  block: AdminEmailBlock;
  onUnblock: (block: AdminEmailBlock) => void;
}) {
  const key = useAdminKey();
  const canUnblock = adminCan(useAdminSession().role, "operator");
  return (
    <TableRow>
      <TableCell className="max-w-48 truncate font-medium sm:max-w-xs">
        {block.email}
      </TableCell>
      <TableCell className="text-zinc-500 max-sm:hidden">
        {formatDate(block.createdAt, "long")}
      </TableCell>
      <TableCell className="text-zinc-500 max-md:hidden">
        {block.linkExpiresAt ? (
          `Works until ${formatDate(block.linkExpiresAt, "long")}`
        ) : (
          <Badge>Expired</Badge>
        )}
      </TableCell>
      <TableCell className="w-0 text-right">
        <Dropdown>
          <DropdownButton plain aria-label={`Actions for ${block.email}`}>
            <EllipsisHorizontalIcon />
          </DropdownButton>
          <DropdownMenu anchor="bottom end">
            <DropdownItem
              href={adminRoutes.events.index(key, {
                email: block.email,
                window: "all",
              })}
            >
              <DropdownLabel>Show activity</DropdownLabel>
            </DropdownItem>
            {canUnblock && (
              <DropdownItem variant="danger" onClick={() => onUnblock(block)}>
                <DropdownLabel>Unblock</DropdownLabel>
              </DropdownItem>
            )}
          </DropdownMenu>
        </Dropdown>
      </TableCell>
    </TableRow>
  );
}
// Unblocking can't be undone from here: there is no admin block, and only
// the owner's link, in one of our emails, can block the address again. A
// 404 (unblocked meanwhile, by its owner or another admin) stays in the
// dialog and reloads the list behind it (onGone).
function UnblockDialog({
  open,
  block,
  onClose,
  onDone,
  onGone,
}: {
  open: boolean;
  block: AdminEmailBlock;
  onClose: () => void;
  onDone: () => void;
  onGone: () => void;
}) {
  const api = useAdminApi();
  async function unblock(reason: string) {
    try {
      await api.emailBlocks().destroy({ email: block.email, reason });
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) onGone();
      throw error;
    }
  }
  return (
    <AdminActionDialog
      open={open}
      onClose={onClose}
      title={`Unblock ${block.email}?`}
      description={`This address will get email from ${appName} again and can be used to sign up. Only its owner can block it again, from the link in one of our emails.`}
      confirm="Unblock"
      variant="danger"
      action={unblock}
      onDone={onDone}
    />
  );
}
