import { Fragment } from "react";
import { MemberLink } from "@/components/app/members/member-link";
import { CopyButton, useCopy } from "@/components/ui/copy-button";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import { Code } from "@/components/ui/text";
import { detailRows, type DetailRow } from "@/lib/event-log";
import { formatDateTime } from "@/lib/format";
import type { WorkspaceEvent } from "@/types/events";
import {
  ApiKeyLink,
  MemberIdLink,
  ProjectLink,
  type EventLinks,
} from "./event-refs";
import { EventWho } from "./event-text";
// A request's ID with a button to copy it, to quote when asking about a
// change: it is the `requestId` of a failed request and in the server's logs.
export function RequestId({ id }: { id: string }) {
  const copy = useCopy(id);
  return (
    <span className="inline-flex items-center gap-x-2">
      <Code className="select-all">{id}</Code>
      <CopyButton copy={copy} label="Copy request ID" />
    </span>
  );
}
// One change as labelled values: when, who, whom it is about, the request
// and the event's own fields, changes as from → to. A key, project or
// member's public ID links to its page where the viewer may open it.
export function EventDetails({
  event,
  links,
}: {
  event: WorkspaceEvent;
  links: EventLinks | null;
}) {
  const member = links?.members ? links.workspace : null;
  return (
    <DescriptionList>
      <DescriptionTerm>When</DescriptionTerm>
      <DescriptionDetails>{formatDateTime(event.createdAt)}</DescriptionDetails>
      <DescriptionTerm>Done by</DescriptionTerm>
      <DescriptionDetails>
        <EventWho event={event} links={links} />
      </DescriptionDetails>
      {event.subject?.name && (
        <>
          <DescriptionTerm>About</DescriptionTerm>
          <DescriptionDetails>
            <MemberLink workspace={member} member={event.subject} />
          </DescriptionDetails>
        </>
      )}
      {detailRows(event.details).map((row) => (
        <Fragment key={row.label}>
          <DescriptionTerm>{row.label}</DescriptionTerm>
          <DescriptionDetails className="break-words">
            <DetailValue event={event} row={row} links={links} />
          </DescriptionDetails>
        </Fragment>
      ))}
      {event.requestId && (
        <>
          <DescriptionTerm>Request ID</DescriptionTerm>
          <DescriptionDetails>
            <RequestId id={event.requestId} />
          </DescriptionDetails>
        </>
      )}
    </DescriptionList>
  );
}
// A detail whose value is a public ID links to that record where the viewer
// may open it: the key, the project or the member. Other values stay text.
function DetailValue({
  event,
  row,
  links,
}: {
  event: WorkspaceEvent;
  row: DetailRow;
  links: EventLinks | null;
}) {
  if (row.field === "key")
    return (
      <ApiKeyLink
        id={row.value}
        personal={event.name.startsWith("auth.api_key.")}
        owner={event.subject?.id}
        links={links}
      />
    );
  if (row.field === "project")
    return <ProjectLink id={row.value} links={links} />;
  if (row.field === "user")
    return <MemberIdLink id={row.value} links={links} />;
  return <>{row.value}</>;
}
