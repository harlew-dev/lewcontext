import { Fragment } from "react";
import { MemberLink } from "@/components/app/members/member-link";
import { CopyButton, useCopy } from "@/components/ui/copy-button";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import { Code } from "@/components/ui/text";
import { detailRows, whoIs } from "@/lib/event-log";
import { formatDateTime } from "@/lib/format";
import type { WorkspaceEvent } from "@/types/events";
// What a list of a workspace's changes links to: the workspace whose member
// and change pages to open, and which of them the viewer may. Null where they
// can't open any (a personal key's own log).
export type EventLinks = {
  workspace: string;
  members: boolean;
  events: boolean;
};
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
// and the event's own fields, changes as from → to. All of it is text.
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
        {event.actor ? (
          <MemberLink workspace={member} member={event.actor} />
        ) : (
          whoIs(null, event.apiKey)
        )}
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
            {row.value}
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
