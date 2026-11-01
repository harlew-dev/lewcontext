import { useState } from "react";
import { EllipsisHorizontalIcon } from "@heroicons/react/16/solid";
import { AdminActionDialog } from "@/components/app/admin/admin-action-dialog";
import { Button } from "@/components/ui/button";
import {
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import {
  Description,
  ErrorMessage,
  Field,
  Label,
} from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Code, Text } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { ApiError, errorMessage } from "@/lib/api";
import { adminPaths, useAdminApi } from "@/lib/api/admin";
import { adminCan, useAdminKey, useAdminSession } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { formatDateTime } from "@/lib/format";
import { useAdminResource } from "@/lib/use-admin-resource";
import { useOpenCount } from "@/lib/use-open-count";
import {
  ipBlockLifetimes,
  type AdminIPBlock,
  type IPBlockLifetime,
} from "@/types/admin";
// Client addresses and ranges every server refuses
// (plans/features/admin/actions.md): each for at most 30 days, at most a
// /16 or /32 wide, never your own address nor a trusted proxy, and never
// the admin dashboard itself, so a mistaken block can be undone here.
// Opening the page is recorded (admin.ip_block.listed); blocking and
// unblocking are operators' actions, behind a fresh code and a reason.
export default function AdminIPBlockIndex() {
  const list = useAdminResource(adminPaths.ipBlocks.index());
  const operator = adminCan(useAdminSession().role, "operator");
  const [blocking, setBlocking] = useState(false);
  const blockOpened = useOpenCount(blocking);
  const [unblocking, setUnblocking] = useState<AdminIPBlock | null>(null);
  const unblockOpened = useOpenCount(unblocking !== null);
  // The last one stays rendered while its dialog animates out.
  const [shown, setShown] = useState<AdminIPBlock | null>(null);
  if (unblocking && unblocking !== shown) setShown(unblocking);
  const blocks = list.data?.data;
  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <Heading>IP blocks</Heading>
        {operator && (
          <Button onClick={() => setBlocking(true)}>Block an address</Button>
        )}
      </div>
      <Text className="mt-2 max-w-2xl">
        Requests from these addresses get “Access from your network has been
        blocked.” on every page and API call, apart from this dashboard. Each
        block ends by itself; for a flood, block at the edge (Cloudflare or your
        proxy) instead.
      </Text>
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      {blocks ? (
        blocks.length === 0 ? (
          <EmptyState
            title="No blocked addresses"
            description="A blocked address or range appears here until it expires."
          />
        ) : (
          <BlockTable
            blocks={blocks}
            operator={operator}
            onUnblock={setUnblocking}
          />
        )
      ) : (
        list.loading && (
          <div className="mt-10">
            <ListSkeleton label="Loading IP blocks" />
          </div>
        )
      )}
      <BlockDialog
        key={blockOpened}
        open={blocking}
        onClose={() => setBlocking(false)}
        onDone={list.refresh}
      />
      {shown && (
        <UnblockDialog
          key={unblockOpened}
          open={unblocking !== null}
          block={shown}
          onClose={() => setUnblocking(null)}
          onDone={list.refresh}
        />
      )}
    </>
  );
}
function BlockTable({
  blocks,
  operator,
  onUnblock,
}: {
  blocks: AdminIPBlock[];
  operator: boolean;
  onUnblock: (block: AdminIPBlock) => void;
}) {
  const key = useAdminKey();
  return (
    <Table className="mt-10">
      <TableHead>
        <TableRow>
          <TableHeader>Network</TableHeader>
          <TableHeader className="max-md:hidden">Reason</TableHeader>
          <TableHeader className="max-sm:hidden">By</TableHeader>
          <TableHeader>Expires</TableHeader>
          <TableHeader className="w-0">
            <span className="sr-only">Actions</span>
          </TableHeader>
        </TableRow>
      </TableHead>
      <TableBody>
        {blocks.map((block) => (
          <TableRow key={block.id}>
            <TableCell>
              <Code>{block.network}</Code>
            </TableCell>
            <TableCell className="max-w-sm whitespace-normal text-zinc-500 max-md:hidden">
              <span className="line-clamp-2 wrap-anywhere">{block.reason}</span>
            </TableCell>
            <TableCell className="text-zinc-500 max-sm:hidden">
              {block.createdBy?.name ?? "A deleted account"}
            </TableCell>
            <TableCell className="text-zinc-500">
              {formatDateTime(block.expiresAt)}
            </TableCell>
            <TableCell className="w-0 text-right">
              <Dropdown>
                <DropdownButton
                  plain
                  aria-label={`Actions for ${block.network}`}
                >
                  <EllipsisHorizontalIcon />
                </DropdownButton>
                <DropdownMenu anchor="bottom end">
                  <DropdownItem
                    href={adminRoutes.events.index(key, {
                      ip: block.network,
                      window: "all",
                    })}
                  >
                    <DropdownLabel>Show activity</DropdownLabel>
                  </DropdownItem>
                  {operator && (
                    <DropdownItem
                      variant="danger"
                      onClick={() => onUnblock(block)}
                    >
                      <DropdownLabel>Unblock</DropdownLabel>
                    </DropdownItem>
                  )}
                </DropdownMenu>
              </Dropdown>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
const lifetimeLabels: Record<IPBlockLifetime, string> = {
  "1h": "1 hour",
  "24h": "24 hours",
  "7d": "7 days",
  "30d": "30 days",
};
// Blocking: an address (one /32 or /128) or a range written from its first
// address, how long, and why. The API refuses ranges wider than a /16 (or
// /32 for IPv6), your own address and trusted proxies, on the field.
function BlockDialog({
  open,
  onClose,
  onDone,
}: {
  open: boolean;
  onClose: () => void;
  onDone: () => void;
}) {
  const api = useAdminApi();
  const [network, setNetwork] = useState("");
  const [expiresIn, setExpiresIn] = useState<IPBlockLifetime>("24h");
  const [errors, setErrors] = useState<{
    network?: string;
    expiresIn?: string;
  }>({});
  function fieldError(error: unknown) {
    if (
      error instanceof ApiError &&
      (error.field === "network" || error.field === "expiresIn")
    ) {
      setErrors({ [error.field]: errorMessage(error) });
      return true;
    }
    return false;
  }
  return (
    <AdminActionDialog
      open={open}
      onClose={onClose}
      title="Block an address?"
      description="Its requests get a 403 on every server within 30 seconds, until the block ends or is lifted. This dashboard stays reachable from it."
      confirm="Block"
      variant="danger"
      ready={network.trim() !== ""}
      onError={fieldError}
      action={async (reason) => {
        setErrors({});
        const block = await api
          .ipBlocks()
          .store({ network, expiresIn, reason });
        toast.success(`${block.network} is blocked.`);
      }}
      onDone={onDone}
    >
      <Field>
        <Label>Address or range</Label>
        <Description>
          An address, or a range from its first address, such as 203.0.113.0/24
          or 2001:db8:1:2::/64. At most a /16 (IPv4) or /32 (IPv6).
        </Description>
        <Input
          name="network"
          value={network}
          invalid={Boolean(errors.network)}
          onChange={(e) => setNetwork(e.currentTarget.value)}
          autoComplete="off"
        />
        {errors.network && (
          <ErrorMessage role="alert">{errors.network}</ErrorMessage>
        )}
      </Field>
      <Field>
        <Label>Lasts</Label>
        <Listbox
          value={expiresIn}
          onChange={(value) => setExpiresIn(value as IPBlockLifetime)}
        >
          {ipBlockLifetimes.map((lifetime) => (
            <ListboxOption key={lifetime} value={lifetime}>
              <ListboxLabel>{lifetimeLabels[lifetime]}</ListboxLabel>
            </ListboxOption>
          ))}
        </Listbox>
        {errors.expiresIn && (
          <ErrorMessage role="alert">{errors.expiresIn}</ErrorMessage>
        )}
      </Field>
    </AdminActionDialog>
  );
}
// Lifting a block before it ends. A 404 means it ended meanwhile: the list
// reloads behind the dialog.
function UnblockDialog({
  open,
  block,
  onClose,
  onDone,
}: {
  open: boolean;
  block: AdminIPBlock;
  onClose: () => void;
  onDone: () => void;
}) {
  const api = useAdminApi();
  async function unblock(reason: string) {
    try {
      await api.ipBlocks().destroy(block.id, reason);
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) onDone();
      throw error;
    }
  }
  return (
    <AdminActionDialog
      open={open}
      onClose={onClose}
      title={`Unblock ${block.network}?`}
      description="Its requests are answered again, on every server within 30 seconds."
      confirm="Unblock"
      variant="danger"
      action={unblock}
      onDone={() => {
        toast.success(`${block.network} is unblocked.`);
        onDone();
      }}
    />
  );
}
