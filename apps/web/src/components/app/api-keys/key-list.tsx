import { useLocation, useSearchParams } from "react-router";
import {
  EllipsisHorizontalIcon,
  EyeIcon,
  NoSymbolIcon,
  PencilSquareIcon,
} from "@heroicons/react/16/solid";
import { MemberLink } from "@/components/app/members/member-link";
import { KeyAccess, KeyStatus } from "./key-display";
import { useKeyActions } from "./key-actions";
import { Button } from "@/components/ui/button";
import {
  Dropdown,
  DropdownButton,
  DropdownMenu,
  DropdownItem,
} from "@/components/ui/dropdown";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Subheading } from "@/components/ui/heading";
import { PaginatedTable } from "@/components/ui/paginated-table";
import { SearchField } from "@/components/ui/search-field";
import { TableRow, TableHeader, TableCell } from "@/components/ui/table";
import { TextLink } from "@/components/ui/text";
import { paths } from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import { routes } from "@/lib/routes";
import { page, cleanSearch, url } from "@/lib/pagination";
import { useResource } from "@/lib/use-resource";
import { apiKeyActive, type ApiKey } from "@/types/api-keys";
import { can, type Workspace } from "@/types/workspaces";

// Shared metadata presentation; each page supplies its own title/explanation.
export function KeyList({
  workspace,
  kind,
  owner,
  title,
  allowCreate = true,
}: {
  workspace?: Workspace;
  kind?: ApiKey["kind"];
  // Only this member's personal keys (a member's page), by their usr_ ID.
  owner?: string;
  title: string;
  allowCreate?: boolean;
}) {
  const { pathname } = useLocation();
  const [search, setSearch] = useSearchParams();
  const prefix = kind === "personal" && workspace ? "personal" : "";
  const q = cleanSearch(search.get(`${prefix}q`) ?? "");
  const current = page(
    new URLSearchParams({ page: search.get(`${prefix}page`) ?? "1" }),
  );
  const list = useResource(
    paths.apiKeys(workspace?.id).index({ page: current, q, kind, owner }),
  );
  const canOpenMembers = workspace
    ? can(workspace, "workspace.member.show")
    : false;
  const actions = useKeyActions(workspace, () => void list.refresh());
  const pageHref = (n: number) => {
    const params = Object.fromEntries(search);
    params[`${prefix}page`] = String(n);
    return url(pathname, params);
  };
  return (
    <section className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <Subheading>{title}</Subheading>
        <div className="flex flex-wrap items-center gap-3">
          {allowCreate && <Button onClick={actions.create}>Add API Key</Button>}
          <SearchField
            label={`Search ${title.toLowerCase()}`}
            value={q}
            onSearch={(value) => {
              const next = new URLSearchParams(search);
              next.delete(`${prefix}page`);
              if (value) next.set(`${prefix}q`, value);
              else next.delete(`${prefix}q`);
              setSearch(next, { replace: true, preventScrollReset: true });
            }}
          />
        </div>
      </div>
      {list.error && (
        <Notice variant="error" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      <PaginatedTable
        page={list.data}
        loading={list.loading}
        label={title}
        href={pageHref}
        rowKey={(key) => key.id}
        head={
          <TableRow>
            <TableHeader>Name</TableHeader>
            <TableHeader>Status</TableHeader>
            <TableHeader className="max-md:hidden">Access</TableHeader>
            <TableHeader className="max-lg:hidden">
              {workspace ? "Owner" : "Workspace"}
            </TableHeader>
            <TableHeader className="max-xl:hidden">Last used</TableHeader>
            <TableHeader>
              <span className="sr-only">Actions</span>
            </TableHeader>
          </TableRow>
        }
        row={(key) => (
          <KeyRow
            apiKey={key}
            workspace={workspace}
            canOpenMembers={canOpenMembers}
            onEdit={() => actions.edit(key)}
            onRevoke={() => actions.revoke(key)}
          />
        )}
        empty={
          <EmptyState
            title={q ? "No matching API keys" : "No API keys"}
            description={
              q
                ? "Try a different search."
                : "Create a key for a server integration or personal script."
            }
          />
        }
      />
      {actions.dialogs}
    </section>
  );
}

function KeyRow({
  apiKey: key,
  workspace,
  canOpenMembers,
  onEdit,
  onRevoke,
}: {
  apiKey: ApiKey;
  workspace?: Workspace;
  canOpenMembers: boolean;
  onEdit: () => void;
  onRevoke: () => void;
}) {
  const active = apiKeyActive(key);
  const page = workspace
    ? routes.workspaceApiKeys.show(workspace.id, key.id)
    : routes.userApiKeys.show(key.id);
  const person = key.owner ?? key.createdBy;
  return (
    <TableRow>
      <TableCell className="max-w-0 min-w-32">
        <TextLink href={page} className="block truncate font-medium">
          {key.name}
        </TextLink>
      </TableCell>
      <TableCell>
        <KeyStatus apiKey={key} />
      </TableCell>
      <TableCell className="max-w-0 min-w-32 max-md:hidden">
        <KeyAccess apiKey={key} />
      </TableCell>
      <TableCell className="max-w-0 min-w-24 max-lg:hidden">
        <span className="block truncate">
          {workspace ? (
            <MemberLink
              workspace={canOpenMembers ? workspace.id : null}
              member={person}
            />
          ) : (
            key.workspace.name
          )}
        </span>
      </TableCell>
      <TableCell className="whitespace-nowrap text-zinc-500 max-xl:hidden dark:text-zinc-400">
        {key.lastUsedAt ? formatDateTime(key.lastUsedAt) : "Never"}
      </TableCell>
      <TableCell className="w-0 text-right">
        <Dropdown>
          <DropdownButton plain aria-label={`Actions for ${key.name}`}>
            <EllipsisHorizontalIcon data-slot="icon" />
          </DropdownButton>
          <DropdownMenu anchor="bottom end">
            <DropdownItem href={page}>
              <EyeIcon />
              View
            </DropdownItem>
            {active && (!workspace || key.kind === "workspace") && (
              <DropdownItem onClick={onEdit}>
                <PencilSquareIcon />
                Edit
              </DropdownItem>
            )}
            {active && (
              <DropdownItem variant="danger" onClick={onRevoke}>
                <NoSymbolIcon />
                Revoke
              </DropdownItem>
            )}
          </DropdownMenu>
        </Dropdown>
      </TableCell>
    </TableRow>
  );
}
