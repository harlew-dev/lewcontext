import { useState } from "react";
import { useLocation, useSearchParams } from "react-router";
import { EllipsisHorizontalIcon } from "@heroicons/react/16/solid";
import { KeyDialog } from "./key-dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
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
import { Text } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { paths, useApi } from "@/lib/api";
import { formatDate, formatDateTime } from "@/lib/format";
import { page, cleanSearch, url } from "@/lib/pagination";
import { useResource } from "@/lib/use-resource";
import { apiKeyActive, type ApiKey } from "@/types/api-keys";
import type { Workspace } from "@/types/workspaces";

// Shared metadata presentation; each page supplies its own title/explanation.
export function KeyList({
  workspace,
  kind,
  title,
  allowCreate = true,
}: {
  workspace?: Workspace;
  kind?: ApiKey["kind"];
  title: string;
  allowCreate?: boolean;
}) {
  const api = useApi();
  const { pathname } = useLocation();
  const [search, setSearch] = useSearchParams();
  const prefix = kind === "personal" && workspace ? "personal" : "";
  const q = cleanSearch(search.get(`${prefix}q`) ?? "");
  const current = page(
    new URLSearchParams({ page: search.get(`${prefix}page`) ?? "1" }),
  );
  const list = useResource(
    paths.apiKeys(workspace?.id).index({ page: current, q, kind }),
  );
  const [dialog, setDialog] = useState<{ editing?: ApiKey } | null>(null);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);
  const reload = () => {
    void list.refresh();
  };
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
          {allowCreate && (
            <Button onClick={() => setDialog({})}>Add API Key</Button>
          )}
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
            <TableHeader>Name / identifier</TableHeader>
            <TableHeader>Access / permissions</TableHeader>
            <TableHeader>
              {workspace ? "Owner / creator" : "Workspace"}
            </TableHeader>
            <TableHeader>Created / expiry</TableHeader>
            <TableHeader>Last used</TableHeader>
            <TableHeader>
              <span className="sr-only">Actions</span>
            </TableHeader>
          </TableRow>
        }
        row={(key) => (
          <KeyRow
            apiKey={key}
            workspace={workspace}
            onEdit={() => setDialog({ editing: key })}
            onRevoke={() => setRevoking(key)}
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
      {dialog && (
        <KeyDialog
          workspace={workspace}
          editing={dialog.editing}
          onClose={() => setDialog(null)}
          onSaved={reload}
        />
      )}
      {revoking && (
        <ConfirmationDialog
          open
          onClose={() => setRevoking(null)}
          title={`Revoke ${revoking.name}?`}
          description="Integrations using this key will stop working immediately."
          confirm="Revoke API key"
          variant="danger"
          onConfirm={() => api.apiKeys(workspace?.id).destroy(revoking.id)}
          onConfirmed={() => {
            setRevoking(null);
            reload();
            toast.success("API key revoked.");
          }}
        />
      )}
    </section>
  );
}

function KeyRow({
  apiKey: key,
  workspace,
  onEdit,
  onRevoke,
}: {
  apiKey: ApiKey;
  workspace?: Workspace;
  onEdit: () => void;
  onRevoke: () => void;
}) {
  const active = apiKeyActive(key);
  return (
    <TableRow>
      <TableCell>
        <div className="font-medium">{key.name}</div>
        <code className="text-xs">
          {key.kind} · {key.id}
        </code>
        <div>
          <Badge>
            {key.revokedAt ? "Revoked" : active ? "Active" : "Expired"}
          </Badge>
        </div>
      </TableCell>
      <TableCell>
        <div>
          {key.projectAccess === "all"
            ? "All projects"
            : key.projects.length
              ? `${key.projects.length} selected projects`
              : "No selected projects remain"}
        </div>
        <Text>{key.scopes.join(", ")}</Text>
        {key.kind === "personal" &&
          key.scopes.some((scope) => !key.effectiveScopes.includes(scope)) && (
            <Text>
              Some actions are unavailable under the owner’s current role.
            </Text>
          )}
      </TableCell>
      <TableCell>
        {workspace
          ? ((key.owner ?? key.createdBy)?.name ?? "Deleted account")
          : key.workspace.name}
      </TableCell>
      <TableCell>
        <div>{formatDate(key.createdAt)}</div>
        <Text>
          Expires: {key.expiresAt ? formatDateTime(key.expiresAt) : "Never"}
        </Text>
      </TableCell>
      <TableCell>
        {key.lastUsedAt ? formatDateTime(key.lastUsedAt) : "Never"}
      </TableCell>
      <TableCell className="w-0 text-right">
        {active && (
          <Dropdown>
            <DropdownButton plain aria-label={`Actions for ${key.name}`}>
              <EllipsisHorizontalIcon data-slot="icon" />
            </DropdownButton>
            <DropdownMenu>
              {(!workspace || key.kind === "workspace") && (
                <DropdownItem onClick={onEdit}>Edit</DropdownItem>
              )}
              <DropdownItem variant="danger" onClick={onRevoke}>
                Revoke
              </DropdownItem>
            </DropdownMenu>
          </Dropdown>
        )}
      </TableCell>
    </TableRow>
  );
}
