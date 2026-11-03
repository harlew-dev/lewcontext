import { useEffect, useState } from "react";
import { useParams, useRevalidator } from "react-router";
import { useSWRConfig } from "swr";
import { AdminActionDialog } from "@/components/app/admin/admin-action-dialog";
import { IncidentThread } from "@/components/app/admin/incident-thread";
import {
  AlertBadges,
  alertValue,
  openFor,
  signalHref,
} from "@/components/app/admin/alerts";
import { eventsUrl } from "@/lib/admin-routes";
import { Button } from "@/components/ui/button";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import { Field, Label } from "@/components/ui/fieldset";
import { ListSkeleton, Notice } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { Code, Text, TextLink } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { errorMessage } from "@/lib/api";
import { adminCan, useAdminKey, useAdminSession } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { adminPaths, useAdminApi } from "@/lib/api/admin";
import { formatDateTime } from "@/lib/format";
import { useAction } from "@/lib/use-action";
import { useAdminResource } from "@/lib/use-admin-resource";
import { useOpenCount } from "@/lib/use-open-count";
import {
  alertSilences,
  type AdminAlert,
  type AdminAlertDetail,
  type AlertSilence,
} from "@/types/admin";
// One alert (docs/api/monitoring.md#alerts): its signal, level, value and
// peak, when it opened and how long it was open, who acknowledged or
// silenced it, and where to look: the Monitoring page of its signal, the
// Events rows behind it, and an integrity check's sample IDs. Opening it is
// one recorded read (admin.alert.viewed). Operators acknowledge it (stops
// its reminder emails: housekeeping) and silence its signal (an action:
// a fresh code and a reason).
export default function Show() {
  const alert = useParams().alert ?? "";
  const resource = useAdminResource(adminPaths.alerts.show(alert));
  return (
    <>
      <Heading>{resource.data?.title ?? "Alert"}</Heading>
      {resource.error && (
        <Notice
          variant="error"
          className="mt-6"
          onRetry={resource.missing ? undefined : resource.refresh}
        >
          {resource.missing ? "Alert not found." : resource.error}
        </Notice>
      )}
      {resource.data ? (
        <Detail alert={resource.data} onChanged={resource.refresh} />
      ) : (
        !resource.error &&
        resource.loading && (
          <div className="mt-10">
            <ListSkeleton label="Loading the alert" />
          </div>
        )
      )}
    </>
  );
}
function Detail({
  alert: loaded,
  onChanged,
}: {
  alert: AdminAlertDetail;
  onChanged: () => void;
}) {
  const key = useAdminKey();
  const api = useAdminApi();
  const session = useAdminSession();
  const operator = adminCan(session.role, "operator");
  const { mutate } = useSWRConfig();
  const revalidator = useRevalidator();
  const state = useAlertState(loaded);
  const alert: AdminAlertDetail = {
    ...loaded,
    state,
    currentLevel: state === "unknown" ? "unknown" : loaded.currentLevel,
  };
  function changed(refresh = true) {
    if (refresh) onChanged();
    // Only this admin's cached alert lists and the layout's sidebar count.
    const index = adminPaths.alerts.index();
    void mutate(
      (cacheKey) =>
        Array.isArray(cacheKey) &&
        cacheKey[0] === "admin" &&
        cacheKey[1] === session.user.id &&
        typeof cacheKey[2] === "string" &&
        (cacheKey[2] === index || cacheKey[2].startsWith(`${index}?`)),
    );
    void revalidator.revalidate();
  }
  const [silencing, setSilencing] = useState(false);
  const opened = useOpenCount(silencing);
  const acknowledge = useAction(() => api.alerts().update(alert.id), {
    onError: (e) => {
      toast.error(errorMessage(e));
      return true;
    },
  });
  async function acknowledged() {
    if (!(await acknowledge.run()).ok) return;
    toast.success("Acknowledged. It won’t be emailed again.");
    changed();
  }
  const open = alert.resolvedAt === null;
  const href = signalHref(key, alert.page);
  const drill = alert.details.drill;
  const samples = alert.details.samples ?? [];
  return (
    <>
      <div className="mt-3 flex flex-wrap items-center justify-between gap-4">
        <AlertBadges alert={alert} />
        {operator && open && (
          <div className="flex gap-3">
            <Button
              outline
              disabled={alert.acknowledgedAt !== null}
              disabledReason="Already acknowledged"
              loading={acknowledge.loading}
              onClick={acknowledged}
            >
              Acknowledge
            </Button>
            <Button outline onClick={() => setSilencing(true)}>
              Silence
            </Button>
          </div>
        )}
      </div>
      <Text className="mt-4 max-w-2xl">{alert.meaning}</Text>
      <DescriptionList className="mt-8">
        <DescriptionTerm>Signal</DescriptionTerm>
        <DescriptionDetails>
          <Code>{alert.signal}</Code>
        </DescriptionDetails>
        <DescriptionTerm>Value now</DescriptionTerm>
        <DescriptionDetails>
          {alertValue(alert, alert.lastValue)} (peak{" "}
          {alertValue(alert, alert.peakValue)})
        </DescriptionDetails>
        <DescriptionTerm>Current health</DescriptionTerm>
        <DescriptionDetails>
          {alert.state}. Current effective level: {alert.currentLevel}; raw
          signal level: {alert.rawLevel}. Cycle highest severity: {alert.level}.
        </DescriptionDetails>
        <DescriptionTerm>Recovery evidence</DescriptionTerm>
        <DescriptionDetails>
          {alert.healthySince ? (
            <>
              Below alert threshold since {formatDateTime(alert.healthySince)};{" "}
              {alert.healthyChecks} distinct healthy checks. Observed through{" "}
              {alert.lastSuccessfulAt
                ? formatDateTime(alert.lastSuccessfulAt)
                : "unknown"}
              .
            </>
          ) : (
            "No continuous below-threshold evidence."
          )}
          {alert.recoveredAt && (
            <> Confirmed {formatDateTime(alert.recoveredAt)}.</>
          )}
        </DescriptionDetails>
        <DescriptionTerm>Check cadence</DescriptionTerm>
        <DescriptionDetails>
          {alert.hourly
            ? "Hourly; one successful healthy reading confirms recovery"
            : "Every minute; 11 healthy checks spanning 10 minutes confirm recovery"}
          .
        </DescriptionDetails>
        <DescriptionTerm>Last check</DescriptionTerm>
        <DescriptionDetails>
          {alert.lastCheckedAt
            ? formatDateTime(alert.lastCheckedAt)
            : "Not checked"}
          . Last successful:{" "}
          {alert.lastSuccessfulAt
            ? formatDateTime(alert.lastSuccessfulAt)
            : "unknown"}
          .
        </DescriptionDetails>
        <DescriptionTerm>Opened</DescriptionTerm>
        <DescriptionDetails>
          {formatDateTime(alert.openedAt)}
        </DescriptionDetails>
        <DescriptionTerm>{open ? "Open for" : "Was open for"}</DescriptionTerm>
        <DescriptionDetails>{openFor(alert)}</DescriptionDetails>
        {alert.resolvedAt && (
          <>
            <DescriptionTerm>Resolved</DescriptionTerm>
            <DescriptionDetails>
              {formatDateTime(alert.resolvedAt)}
            </DescriptionDetails>
          </>
        )}
        <DescriptionTerm>Told by</DescriptionTerm>
        <DescriptionDetails>{toldBy(alert)}</DescriptionDetails>
        {alert.acknowledgedAt && (
          <>
            <DescriptionTerm>Acknowledged</DescriptionTerm>
            <DescriptionDetails>
              {formatDateTime(alert.acknowledgedAt)}
              {alert.acknowledgedBy && ` by ${alert.acknowledgedBy.name}`}
            </DescriptionDetails>
          </>
        )}
        {alert.silencedUntil && (
          <>
            <DescriptionTerm>Silenced until</DescriptionTerm>
            <DescriptionDetails>
              {formatDateTime(alert.silencedUntil)}
              {alert.silencedBy && ` by ${alert.silencedBy.name}`}
            </DescriptionDetails>
          </>
        )}
      </DescriptionList>
      <div className="mt-8 flex flex-wrap gap-x-6 gap-y-2 text-sm/6">
        {href && <TextLink href={href}>Its Monitoring page</TextLink>}
        {drill && (
          <TextLink
            href={eventsUrl(`/admin/${key}/events`, {
              name: drill.name,
              outcome: drill.outcome,
              window: drill.window,
            })}
          >
            The events behind it
          </TextLink>
        )}
        <TextLink href={adminRoutes.alerts.index(key)}>All alerts</TextLink>
      </div>
      {samples.length > 0 && (
        <>
          <Text className="mt-8">Rows that broke the check (public IDs):</Text>
          <ul className="mt-2 flex flex-wrap gap-2">
            {samples.map((id) => (
              <li key={id}>
                <Code>{id}</Code>
              </li>
            ))}
          </ul>
        </>
      )}
      <IncidentThread
        key={alert.id}
        kind="alert"
        parent={alert.id}
        thread={alert}
        canResolve={
          state === "recovered" &&
          alert.currentLevel === "ok" &&
          alert.recoveredAt !== null
        }
        resolveReason={resolutionReason(state)}
        onResolve={async (cycle, message) => {
          await api.alerts().update(alert.id, {
            resolved: true,
            cycle,
            message,
            revision: alert.revision,
          });
          changed(false);
        }}
        onChanged={onChanged}
      />
      <SilenceDialog
        key={opened}
        open={silencing}
        alert={alert}
        onClose={() => setSilencing(false)}
        onDone={() => changed()}
      />
    </>
  );
}
// Expire the displayed evidence locally without polling or recording another
// read. The server independently checks freshness using the database clock.
function useAlertState(alert: AdminAlert): AdminAlert["state"] {
  const [now, setNow] = useState(Date.now);
  const observed = alert.lastSuccessfulAt
    ? Date.parse(alert.lastSuccessfulAt)
    : NaN;
  const deadline = observed + (alert.hourly ? 65 : 2) * 60_000;
  useEffect(() => {
    const remaining = deadline - Date.now();
    if (alert.state === "resolved") return;
    const update = () => setNow(Date.now());
    // Rebase after new evidence arrives, then wake once when it expires.
    const current = window.setTimeout(update, 0);
    const expiry =
      Number.isFinite(remaining) && remaining >= 0
        ? window.setTimeout(() => setNow(deadline + 1), remaining + 1)
        : undefined;
    return () => {
      window.clearTimeout(current);
      window.clearTimeout(expiry);
    };
  }, [deadline, alert.state]);
  if (alert.state === "resolved") return "resolved";
  return !Number.isFinite(deadline) || now < observed || now > deadline
    ? "unknown"
    : alert.state;
}
function resolutionReason(state: AdminAlert["state"]): string | undefined {
  switch (state) {
    case "active":
      return "This alert is still active. Resolution requires fresh, confirmed recovery.";
    case "confirming":
      return "Recovery is still being confirmed. Wait for the required healthy checks before resolving.";
    case "unknown":
      return "Health evidence is unavailable or stale. Reload to check for fresh, confirmed recovery before resolving.";
    default:
      return undefined;
  }
}
// How its opening was told, in words.
function toldBy(alert: AdminAlert): string {
  switch (alert.notified) {
    case "mail":
      return "Email and webhook";
    case "digest":
      return "A digest email and the webhook";
    case "held":
      return "The webhook, then a digest email";
    default:
      return "No one: it was silenced, or its signal had just alerted";
  }
}
// Silencing the alert's signal: no email or webhook about it for up to 24
// hours, nor about another alert of it opening meanwhile.
function SilenceDialog({
  open,
  alert,
  onClose,
  onDone,
}: {
  open: boolean;
  alert: AdminAlert;
  onClose: () => void;
  onDone: () => void;
}) {
  const api = useAdminApi();
  const [duration, setDuration] = useState<AlertSilence>("4h");
  return (
    <AdminActionDialog
      open={open}
      onClose={onClose}
      title={`Silence ${alert.title}?`}
      description="No email or webhook about this signal until the time is up, however it changes. Silencing hides what may be a real attack: say why."
      confirm="Silence"
      action={(reason) => api.alertSilence(alert.id).store(duration, reason)}
      onDone={() => {
        toast.success("Silenced.");
        onDone();
      }}
    >
      <Field>
        <Label>For</Label>
        <Listbox
          value={duration}
          onChange={(value) => setDuration(value as AlertSilence)}
        >
          {alertSilences.map((s) => (
            <ListboxOption key={s.value} value={s.value}>
              <ListboxLabel>{s.label}</ListboxLabel>
            </ListboxOption>
          ))}
        </Listbox>
      </Field>
    </AdminActionDialog>
  );
}
