import { Fragment } from "react";
import { MemberLink } from "@/components/app/members/member-link";
import { eventSentenceParts } from "@/lib/event-log";
import type { WorkspaceEvent } from "@/types/events";
import { ApiKeyLink, type EventLinks } from "./event-refs";
// Who did it: their name linking to their page, an API key's ID linking to
// the key's page where the viewer may open it, or the stand-in for a deleted
// account. The caller sets the weight.
export function EventWho({
  event,
  links,
}: {
  event: WorkspaceEvent;
  links: EventLinks | null;
}) {
  if (event.actor)
    return (
      <MemberLink
        workspace={links?.members ? links.workspace : null}
        member={event.actor}
      />
    );
  if (event.apiKey)
    return (
      <>
        API key <ApiKeyLink id={event.apiKey} links={links} />
      </>
    );
  return <>A deleted account</>;
}
// What happened, after who did it, with each API key's ID linking to its page
// where the viewer may open it.
export function EventSentence({
  event,
  links,
}: {
  event: WorkspaceEvent;
  links: EventLinks | null;
}) {
  return (
    <>
      {eventSentenceParts(event).map((part, index) =>
        "text" in part ? (
          <Fragment key={index}>{part.text}</Fragment>
        ) : (
          <ApiKeyLink
            key={index}
            id={part.key}
            personal={part.personal}
            owner={event.subject?.id}
            links={links}
          />
        ),
      )}
    </>
  );
}
