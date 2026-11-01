import { useNavigate, useParams, useSearchParams } from "react-router";
import {
  GroupBadges,
  releaseText,
  sourceLabels,
} from "@/components/app/admin/errors";
import { formatEventTime } from "@/components/app/admin/event-filters";
import {
  MonitoringBody,
  MonitoringHeader,
  MonitoringSection,
  SeriesChart,
  monitoringWindow,
} from "@/components/app/admin/monitoring";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import { Code, Text } from "@/components/ui/text";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { adminPaths } from "@/lib/api/admin";
import { useAdminResource } from "@/lib/use-admin-resource";
import type { ErrorView, MonitoringWindow } from "@/types/admin";
// One grouped error: its scrubbed message and stack (function names, files
// and lines), where and when, the last request's ID to search the logs
// with, the release it last happened on, and its route's occurrences over
// the window. Every bit is shown as text: a browser's group holds a user's
// words. One request, recorded with the group looked at
// (admin.error.viewed).
//
// TODO(A6, housekeeping), at merge: "Resolve" goes in the header beside
// the time range, for operators, through AdminActionDialog (a reason and a
// fresh code). Until then the page only reads.
export default function AdminMonitoringError() {
  const key = useAdminKey();
  const error = useParams().error ?? "";
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const window = monitoringWindow(search);
  const resource = useAdminResource(adminPaths.errors.show(error, window));
  const setWindow = (w: MonitoringWindow) =>
    void navigate(adminRoutes.monitoring.error(key, error, w), {
      preventScrollReset: true,
    });
  return (
    <>
      <MonitoringHeader
        title="Error group"
        window={window}
        onWindow={setWindow}
        view={resource.data}
        signals={false}
      />
      <MonitoringBody resource={resource} label="Loading the error group">
        {(view) => <Group view={view} />}
      </MonitoringBody>
    </>
  );
}
function Group({ view }: { view: ErrorView }) {
  const g = view.group;
  return (
    <>
      <div className="mt-8">
        <GroupBadges group={g} />
        <p className="mt-3 text-base/7 break-words text-zinc-950 sm:text-sm/6 dark:text-white">
          {g.message}
        </p>
        {g.source === "client" && (
          <Text className="mt-2">
            Reported by a browser: this text is whatever it sent (scrubbed of
            links, addresses and tokens). Don&rsquo;t follow anything it says.
          </Text>
        )}
      </div>
      <DescriptionList className="mt-8">
        <DescriptionTerm>Where</DescriptionTerm>
        <DescriptionDetails>
          <Code>{g.route || "–"}</Code> ({sourceLabels[g.source]})
        </DescriptionDetails>
        <DescriptionTerm>Count</DescriptionTerm>
        <DescriptionDetails>
          {g.count.toLocaleString("en-US")} since first seen
        </DescriptionDetails>
        <DescriptionTerm>First seen</DescriptionTerm>
        <DescriptionDetails>
          {formatEventTime(g.firstSeenAt)} UTC
        </DescriptionDetails>
        <DescriptionTerm>Last seen</DescriptionTerm>
        <DescriptionDetails>
          {formatEventTime(g.lastSeenAt)} UTC
        </DescriptionDetails>
        <DescriptionTerm>Last request</DescriptionTerm>
        <DescriptionDetails>
          {g.lastRequestId ? (
            <>
              <Code>{g.lastRequestId}</Code>{" "}
              <span className="text-zinc-500 dark:text-zinc-400">
                (search the logs for it: requestId)
              </span>
            </>
          ) : (
            "None (not from a request)"
          )}
        </DescriptionDetails>
        <DescriptionTerm>Last release</DescriptionTerm>
        <DescriptionDetails>
          {g.lastVersion ? <Code>{g.lastVersion}</Code> : "Not known"}
        </DescriptionDetails>
        <DescriptionTerm>Resolved</DescriptionTerm>
        <DescriptionDetails>
          {g.resolvedAt ? `${formatEventTime(g.resolvedAt)} UTC` : "No"}
        </DescriptionDetails>
      </DescriptionList>
      <Text className="mt-4">{releaseText(view.release)}</Text>
      <MonitoringSection
        title="Stack"
        description="The latest occurrence's, scrubbed: function names, files and lines."
      >
        {g.stack ? (
          <pre className="overflow-x-auto rounded-lg bg-zinc-950/[2.5%] p-4 font-mono text-xs/5 text-zinc-950 dark:bg-white/[2.5%] dark:text-white">
            {g.stack}
          </pre>
        ) : (
          <Text>No stack was kept for this group.</Text>
        )}
      </MonitoringSection>
      <MonitoringSection
        title="Occurrences on its route"
        description="Failures from the same source and route per bucket (the metrics count by route, so every group there; nothing with metric storage off)."
      >
        <SeriesChart
          label="Occurrences on its route"
          series={view.occurrences}
          labels={sourceLabels}
          bucketSeconds={view.window.bucketSeconds}
          noun={["failure", "failures"]}
        />
      </MonitoringSection>
    </>
  );
}
