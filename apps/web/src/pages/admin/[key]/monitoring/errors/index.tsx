import {
  GroupBadges,
  releaseText,
  sourceLabels,
} from "@/components/app/admin/errors";
import {
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  MonitoringTable,
  NoneFound,
  SectionGate,
  SeriesChart,
  SignalGrid,
  useMonitoringPage,
} from "@/components/app/admin/monitoring";
import { formatEventTime } from "@/components/app/admin/event-filters";
import { TableCell, TableRow } from "@/components/ui/table";
import { Code, Text, TextLink } from "@/components/ui/text";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import type { ErrorsSections, MonitoringView } from "@/types/admin";
// What's failing, since when, and did the last release cause it? The
// grouped errors seen in the window, newest occurrence first, those first
// seen since the current release marked New, and their occurrences per
// bucket. A group a browser reported holds a user's text: it's labelled
// as user-reported and always shown as text, never a link. One request,
// one recorded read; each group's page is a read of its own.
// Resolving a group is an operator's housekeeping, on the group's page.
export default function AdminMonitoringErrors() {
  const page = useMonitoringPage<ErrorsSections>(
    "errors",
    adminRoutes.monitoring.errors,
  );
  return (
    <>
      <MonitoringHeader
        title="Errors"
        window={page.window}
        onWindow={page.setWindow}
        view={page.data}
      />
      <MonitoringBody resource={page} label="Loading errors">
        {(view) => <Errors view={view} />}
      </MonitoringBody>
    </>
  );
}
type View = MonitoringView<ErrorsSections>;
function Errors({ view }: { view: View }) {
  return (
    <>
      <SignalGrid className="mt-8" signals={view.signals} />
      <SectionGate names={["groups"]} title="Error groups">
        <Groups view={view} />
      </SectionGate>
      <SectionGate names={["occurrences"]} title="Occurrences">
        <Occurrences view={view} />
      </SectionGate>
    </>
  );
}
function Groups({ view }: { view: View }) {
  const key = useAdminKey();
  const { groups, release } = view.sections;
  return (
    <MonitoringSection
      title="Error groups"
      description={`Failures grouped by where and what, last seen in this window (at most 50). ${releaseText(release)} User-reported groups came from a browser: their text is whatever that browser sent.`}
    >
      {groups.length === 0 ? (
        <NoneFound>Nothing failed in this window.</NoneFound>
      ) : (
        <MonitoringTable
          head={["Error", "Where", "Count", "First seen", "Last seen"]}
        >
          {groups.map((g) => (
            <TableRow key={g.id}>
              <TableCell className="max-w-md whitespace-normal">
                <GroupBadges group={g} />
                {/* Always text: a browser's message is a user's. */}
                <div className="mt-1 break-words text-zinc-950 dark:text-white">
                  {g.message}
                </div>
                <TextLink
                  href={adminRoutes.monitoring.error(
                    key,
                    g.id,
                    view.window.name,
                  )}
                  className="text-sm/6"
                >
                  Details
                </TextLink>
              </TableCell>
              <TableCell className="whitespace-normal">
                <Code>{g.route || "–"}</Code>
              </TableCell>
              <TableCell className="tabular-nums">
                {g.count.toLocaleString("en-US")}
              </TableCell>
              <TableCell>{formatEventTime(g.firstSeenAt)}</TableCell>
              <TableCell>{formatEventTime(g.lastSeenAt)}</TableCell>
            </TableRow>
          ))}
        </MonitoringTable>
      )}
      <Text className="mt-3">
        Counts are since each group was first seen. Times are UTC.
      </Text>
    </MonitoringSection>
  );
}
function Occurrences({ view }: { view: View }) {
  return (
    <MonitoringSection
      title="Occurrences"
      description="Every failure reported, per bucket, by where it came from (from the metrics: nothing with metric storage off)."
    >
      <SeriesChart
        label="Occurrences"
        series={view.sections.occurrences}
        labels={sourceLabels}
        bucketSeconds={view.window.bucketSeconds}
        noun={["failure", "failures"]}
      />
    </MonitoringSection>
  );
}
