import { useSearchParams } from "react-router";
import {
  AlertBadges,
  alertValue,
  openFor,
} from "@/components/app/admin/alerts";
import { CursorNav } from "@/components/app/admin/cursor-nav";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { Navbar, NavbarItem, NavbarSection } from "@/components/ui/navbar";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Text, TextLink } from "@/components/ui/text";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { adminPaths } from "@/lib/api/admin";
import { cx } from "@/lib/cx";
import { formatDateTime } from "@/lib/format";
import { useAdminResource } from "@/lib/use-admin-resource";
import { alertStates, type AlertState } from "@/types/admin";
const stateLabels: Record<AlertState, string> = {
  open: "Open",
  resolved: "Resolved",
  all: "All",
};
// The alerts the evaluator opened (docs/api/monitoring.md#alerts): the open
// ones first, critical before warning, then resolved ones newest first. The
// state and the cursor live in the URL; opening the page is one recorded
// read (admin.alert.listed). Acknowledging and silencing are on an alert's
// own page.
export default function AdminAlertIndex() {
  const key = useAdminKey();
  const [search] = useSearchParams();
  const state = alertStates.find((s) => s === search.get("state")) ?? "open";
  const before = search.get("before") ?? undefined;
  const list = useAdminResource(adminPaths.alerts.index({ state, before }));
  const hrefFor = (cursor: string | undefined) => {
    const base = adminRoutes.alerts.index(key, state);
    return cursor
      ? `${base}${base.includes("?") ? "&" : "?"}before=${cursor}`
      : base;
  };
  return (
    <>
      <Heading>Alerts</Heading>
      <Text className="mt-2 max-w-2xl">
        A signal that stays at its warning or critical level opens an alert; it
        resolves after 10 minutes back to normal. Critical alerts are emailed to
        admins who asked for them, and every alert goes to the webhook if one is
        set.
      </Text>
      <Navbar aria-label="Alert states" className="mt-4">
        <NavbarSection className="-ml-2">
          {alertStates.map((s) => (
            <NavbarItem
              key={s}
              href={adminRoutes.alerts.index(key, s)}
              current={state === s}
              aria-current={state === s ? "page" : undefined}
            >
              {stateLabels[s]}
            </NavbarItem>
          ))}
        </NavbarSection>
      </Navbar>
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      {list.data ? (
        list.data.data.length === 0 ? (
          <EmptyState
            title={state === "open" ? "No open alerts" : "No alerts"}
            description="Alerts appear here when a signal stays at its warning or critical level."
          />
        ) : (
          <div className={cx(list.refreshing && "opacity-60")}>
            <Table className="mt-6">
              <TableHead>
                <TableRow>
                  <TableHeader>Alert</TableHeader>
                  <TableHeader>State</TableHeader>
                  <TableHeader>Value</TableHeader>
                  <TableHeader className="max-sm:hidden">Opened</TableHeader>
                  <TableHeader className="max-md:hidden">Open for</TableHeader>
                </TableRow>
              </TableHead>
              <TableBody>
                {list.data.data.map((alert) => (
                  <TableRow key={alert.id}>
                    <TableCell className="whitespace-normal">
                      <TextLink href={adminRoutes.alerts.show(key, alert.id)}>
                        {alert.title}
                      </TextLink>
                    </TableCell>
                    <TableCell>
                      <AlertBadges alert={alert} />
                    </TableCell>
                    <TableCell>
                      {alertValue(alert, alert.lastValue)}
                      {alert.peakValue > alert.lastValue && (
                        <span className="text-zinc-500">
                          {" "}
                          (peak {alertValue(alert, alert.peakValue)})
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="text-zinc-500 max-sm:hidden">
                      {formatDateTime(alert.openedAt)}
                    </TableCell>
                    <TableCell className="text-zinc-500 max-md:hidden">
                      {openFor(alert)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {state !== "open" && (
              <CursorNav
                label="Alert pages"
                next={list.data.next}
                hrefFor={hrefFor}
              />
            )}
          </div>
        )
      ) : (
        list.loading && (
          <div className="mt-10">
            <ListSkeleton label="Loading alerts" />
          </div>
        )
      )}
    </>
  );
}
