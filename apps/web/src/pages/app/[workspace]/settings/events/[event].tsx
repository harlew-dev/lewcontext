import { useParams } from "react-router";
import { EventDetails, RequestId } from "@/components/app/events/event-details";
import { EventSentence, EventWho } from "@/components/app/events/event-text";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import { Heading, Subheading } from "@/components/ui/heading";
import { Text, TextLink } from "@/components/ui/text";
import { paths } from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import { parsePublicId } from "@/lib/ids";
import { routes } from "@/lib/routes";
import { useSession } from "@/lib/session";
import { useResource } from "@/lib/use-resource";
import { useWorkspace } from "@/lib/workspace";
import { can } from "@/types/workspaces";
// One change: what happened, who did it, its details and the request ID to
// quote, and the other changes recorded in the same request. Owners and
// admins only (workspace.event.show). The page's ID is the change's own
// public ID: a request can record several changes, so its ID can't name one.
export default function Show() {
  const workspace = useWorkspace();
  const id = parsePublicId("evt", useParams().event);
  if (!can(workspace, "workspace.event.show"))
    return (
      <Notice variant="error">
        Only owners and admins can see this workspace’s logs.
      </Notice>
    );
  return id === null ? <NotFound /> : <Content key={id} id={id} />;
}
function Content({ id }: { id: string }) {
  const workspace = useWorkspace();
  const session = useSession();
  const event = useResource(paths.events(workspace.id).show(id));
  if (event.missing) return <NotFound />;
  const links = {
    workspace: workspace.id,
    members: can(workspace, "workspace.member.show"),
    events: true,
    apiKeys: can(workspace, "workspace.api_key.show"),
    viewer: session.user.id,
  };
  const data = event.data;
  return (
    <div className="mx-auto max-w-4xl">
      {event.error && (
        <Notice variant="error" className="mt-4" onRetry={event.refresh}>
          {event.error}
        </Notice>
      )}
      {data ? (
        <>
          <Heading className="mt-4">
            <EventWho event={data} links={links} />{" "}
            <EventSentence event={data} links={links} />
          </Heading>
          <Text className="mt-1">{formatDateTime(data.createdAt)}</Text>
          <div className="mt-8">
            <EventDetails event={data} links={links} />
          </div>
          {data.requestId && (
            <div className="mt-10">
              <Subheading>Other changes in this request</Subheading>
              <Text className="mt-1">
                Request <RequestId id={data.requestId} /> recorded these
                together.
              </Text>
              {data.related.length > 0 ? (
                <ul className="mt-4 divide-y divide-zinc-950/5 dark:divide-white/5">
                  {data.related.map((other) => (
                    <li key={other.id} className="py-3 text-sm/6">
                      <EventWho event={other} links={links} />{" "}
                      <EventSentence event={other} links={links} />
                      <span className="ml-2 text-zinc-500 dark:text-zinc-400">
                        <TextLink
                          href={routes.events.show(workspace.id, other.id)}
                        >
                          {formatDateTime(other.createdAt)}
                        </TextLink>
                      </span>
                    </li>
                  ))}
                </ul>
              ) : (
                <Text className="mt-4">Nothing else.</Text>
              )}
            </div>
          )}
        </>
      ) : (
        !event.error && (
          <div className="mt-8">
            <ListSkeleton label="Loading change" rows={4} />
          </div>
        )
      )}
    </div>
  );
}
function NotFound() {
  return (
    <div className="mx-auto max-w-4xl">
      <EmptyState
        title="Change not found"
        description="It may be older than the 90 days changes are kept, or not part of this workspace."
      />
    </div>
  );
}
