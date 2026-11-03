import { useState } from "react";
import { AdminActionDialog } from "@/components/app/admin/admin-action-dialog";
import { Notice } from "@/components/ui/feedback";
import {
  Description,
  ErrorMessage,
  Field,
  Label,
} from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { toast } from "@/components/ui/toast";
import { ApiError, errorMessage } from "@/lib/api";
import { useAdminApi } from "@/lib/api/admin";
import { ipBlockLifetimes, type IPBlockLifetime } from "@/types/admin";
// Blocking a client address or range (docs/api/admin.md#ip-blocks): an
// operator's action, behind a fresh code and a reason (AdminActionDialog).
const lifetimeLabels: Record<IPBlockLifetime, string> = {
  "1h": "1 hour",
  "24h": "24 hours",
  "7d": "7 days",
  "30d": "30 days",
};
// Blocking: an address (one /32 or /128) or a range written from its first
// address, how long, and why. The API refuses ranges wider than a /16 (or
// /32 for IPv6), your own address and trusted proxies, on the field. An
// IPv6 range wider than a /48 is allowed but warned about: it can be a
// whole provider's customers, where one person usually has a /64.
// Opened from the IP blocks page (empty) and from one address's Monitoring
// page (network: that address, or an IPv6 address's /64). Remount it (a
// key) for each opening, so it starts from network again.
export function IPBlockDialog({
  open,
  onClose,
  onDone,
  network: initialNetwork = "",
}: {
  open: boolean;
  onClose: () => void;
  onDone: () => void;
  network?: string;
}) {
  const api = useAdminApi();
  const [network, setNetwork] = useState(initialNetwork);
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
  const wide = wideIPv6Prefix(network);
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
          or 2001:db8:1:2::/64. At most a /16 (IPv4) or /32 (IPv6). One person
          on IPv6 usually has a whole /64: block that rather than a single
          address.
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
        {wide !== null && (
          <Notice variant="warning" className="mt-3">
            A /{wide} of IPv6 is wider than a /48: it can cover a whole
            provider’s customers. For one person, block their /64.
          </Notice>
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
// wideIPv6Prefix is the prefix length of network when it's an IPv6 range
// wider than a /48 (a site's usual allocation), or null.
function wideIPv6Prefix(network: string): number | null {
  const [address, prefix] = network.trim().split("/");
  if (
    !address?.includes(":") ||
    prefix === undefined ||
    !/^\d+$/.test(prefix)
  ) {
    return null;
  }
  const bits = Number(prefix);
  return bits < 48 ? bits : null;
}
