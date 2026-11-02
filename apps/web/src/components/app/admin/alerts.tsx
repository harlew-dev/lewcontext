import { useState, type ReactNode } from "react";
import { Badge, LevelBadge } from "@/components/ui/badge";
import { formatSignal } from "@/components/app/admin/monitoring";
import { adminRoutes } from "@/lib/admin-routes";
import { formatDateTime } from "@/lib/format";
import type { AdminAlert } from "@/types/admin";
// What the Alerts list and one alert's page share
// (docs/api/monitoring.md#alerts).

// An alert's value in its signal's unit.
export function alertValue(alert: AdminAlert, value: number): string {
  return formatSignal({ unit: alert.unit, value });
}
// How long it was or has been open: "2 h 5 min".
export function openFor(alert: AdminAlert, now = Date.now()): string {
  const end = alert.resolvedAt ? new Date(alert.resolvedAt).getTime() : now;
  const minutes = Math.max(
    0,
    Math.floor((end - new Date(alert.openedAt).getTime()) / 60000),
  );
  const hours = Math.floor(minutes / 60);
  if (hours === 0) return minutes < 1 ? "under a minute" : `${minutes} min`;
  return minutes % 60 ? `${hours} h ${minutes % 60} min` : `${hours} h`;
}
// The alert's state in words, with the level as a LevelBadge: Resolved,
// Acknowledged and Silenced are badges beside it.
export function AlertBadges({ alert }: { alert: AdminAlert }): ReactNode {
  // When the page opened: what "silenced" is measured from.
  const [now] = useState(Date.now);
  const silenced =
    alert.silencedUntil !== null &&
    new Date(alert.silencedUntil).getTime() > now;
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5">
      <LevelBadge level={alert.level} />
      {alert.resolvedAt && <Badge color="lime">Resolved</Badge>}
      {!alert.resolvedAt && alert.acknowledgedAt && (
        <Badge color="zinc">Acknowledged</Badge>
      )}
      {!alert.resolvedAt && silenced && <Badge color="purple">Silenced</Badge>}
    </span>
  );
}
// The dashboard page of a signal's page: a Monitoring page, or Mail.
export function signalHref(key: string, page: string): string | null {
  const monitoring: Record<string, string> = {
    overview: adminRoutes.monitoring.index(key),
    security: adminRoutes.monitoring.security(key),
    limits: adminRoutes.monitoring.limits(key),
    ips: adminRoutes.monitoring.ips(key),
    accounts: adminRoutes.monitoring.accounts(key),
    workspaces: adminRoutes.monitoring.workspaces(key),
    usage: adminRoutes.monitoring.usage(key),
    integrity: adminRoutes.monitoring.integrity(key),
    performance: adminRoutes.monitoring.performance(key),
    errors: adminRoutes.monitoring.errors(key),
    configuration: adminRoutes.monitoring.configuration(key),
    mail: adminRoutes.deliveries.index(key),
  };
  return monitoring[page] ?? null;
}
export { formatDateTime };
