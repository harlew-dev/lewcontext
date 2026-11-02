import { useState } from "react";
import { useParams } from "react-router";
import { AdminActionDialog } from "@/components/app/admin/admin-action-dialog";
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
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
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
  type AlertSilence,
} from "@/types/admin";
// One alert (docs/api/monitoring.md#alerts): its signal, level, value and
// peak, when it opened and how long it was open, who acknowledged or
// silenced it, and where to look: the Monitoring page of its signal, the
// Events rows behind it, and an integrity check's sample IDs. Opening it is
// one recorded read (admin.alert.viewed). Operators acknowledge it (stops
// its reminder emails: housekeeping) and silence its signal (an action:
// a fresh code and a reason).
export default function AdminAlertShow() {
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
  alert,
  onChanged,
}: {
  alert: AdminAlert;
  onChanged: () => void;
}) {
  const key = useAdminKey();
  const api = useAdminApi();
  const operator = adminCan(useAdminSession().role, "operator");
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
    onChanged();
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
      {!open && samples.length === 0 && !drill && (
        <EmptyState title="Resolved" description="Nothing more to do here." />
      )}
      <SilenceDialog
        key={opened}
        open={silencing}
        alert={alert}
        onClose={() => setSilencing(false)}
        onDone={onChanged}
      />
    </>
  );
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
