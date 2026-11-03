import { Fragment, useId, useState } from "react";
import { useSearchParams } from "react-router";
import { ChevronRightIcon } from "@heroicons/react/16/solid";
import { MemberLink } from "@/components/app/members/member-link";
import { Button } from "@/components/ui/button";
import { CursorNav } from "@/components/ui/cursor-nav";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import {
  FilterBadges,
  FiltersButton,
  useFocusAfterRemove,
  type FilterBadge,
} from "@/components/ui/filter-bar";
import { SearchField } from "@/components/ui/search-field";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { TextLink } from "@/components/ui/text";
import type { ApiPath, EventIndexQuery } from "@/lib/api";
import { cx } from "@/lib/cx";
import {
  eventLabels,
  eventSentence,
  eventSummary,
  whoIs,
} from "@/lib/event-log";
import { formatDateTime } from "@/lib/format";
import { routes } from "@/lib/routes";
import { useResource } from "@/lib/use-resource";
import {
  defaultEventWindow,
  eventTypes,
  type EventPage,
  type EventQuery,
  type WorkspaceEvent,
} from "@/types/events";
import { EventDetails, type EventLinks } from "./event-details";
import {
  EventFiltersDialog,
  type FilterChoices,
  type FilterPatch,
} from "./event-filters-dialog";
import {
  dateLabel,
  eventQuery,
  eventSearch,
  indexQuery,
} from "./event-filters";
// A list of a workspace's changes with its controls, shared by the Logs tab,
// a member's Logs and an API key's Logs: a search and Filters in the header
// (the date and the other filters live in the dialog), the applied filters as
// badges, then the changes newest first, each opening in place and, through
// its time, to its own page. The state is in the URL (other parameters, a
// page's ?tab=, are kept), so a link opens the same view. Reading it records
// nothing.
export function EventLog({
  label,
  pathFor,
  links,
  choices,
}: {
  // Names the list for screen readers ("Logs").
  label: string;
  // The API path for a query: a workspace's log, narrowed to a member or a
  // key by the page, or a personal key's own.
  pathFor: (query: EventIndexQuery) => ApiPath<EventPage>;
  // Where its members and changes can be opened, or null.
  links: EventLinks | null;
  // The people and projects to filter by, or null for none.
  choices: FilterChoices;
}) {
  const [search, setSearch] = useSearchParams();
  const query = eventQuery(search);
  const list = useResource(pathFor(indexQuery(query)));
  const [dialog, setDialog] = useState<"filters" | null>(null);
  const filtersButton = useId();
  const focusAfter = useFocusAfterRemove(search, filtersButton);
  const go = (patch: Partial<EventQuery>, replace = false) =>
    setSearch(eventSearch(search, { ...query, ...patch, before: "" }), {
      replace,
      preventScrollReset: true,
    });
  const refs = list.data?.refs;
  const badges = appliedBadges(query, refs);
  const narrowed = badges.some((badge) => badge.id !== "date");
  const apply = (patch: FilterPatch) => {
    setDialog(null);
    go(patch);
  };
  return (
    <>
      <div className="flex flex-wrap items-end gap-3">
        <div className="w-full sm:w-64">
          <SearchField
            label="Search changes"
            value={query.q}
            onSearch={(q) => go({ q }, true)}
          />
        </div>
        <FiltersButton
          id={filtersButton}
          count={badges.length}
          onClick={() => setDialog("filters")}
        />
      </div>
      <FilterBadges
        badges={badges}
        onRemove={(id, focus) => {
          focusAfter(focus);
          go(without(id));
        }}
        onClearAll={() => {
          focusAfter("filters");
          go({
            types: [],
            actor: "",
            project: "",
            q: "",
            window: defaultEventWindow,
            from: "",
            to: "",
          });
        }}
        onOpen={() => setDialog("filters")}
      />
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      <div className="mt-6">
        {list.loading && !list.data ? (
          <ListSkeleton label="Loading logs" rows={5} />
        ) : list.data && list.data.data.length > 0 ? (
          <div
            aria-busy={list.refreshing}
            className={cx(list.refreshing && "opacity-60")}
          >
            <EventTable label={label} events={list.data.data} links={links} />
            <CursorNav
              label={`${label} pages`}
              next={list.data.next}
              hrefFor={(before) =>
                `?${eventSearch(search, { ...query, before: before ?? "" })}`
              }
            />
          </div>
        ) : list.data ? (
          <EmptyState
            title={
              narrowed
                ? "No changes match these filters"
                : "No changes in this period"
            }
            description={
              narrowed
                ? "Try a different search, type or person."
                : "Changes to the workspace, its members, invitations, projects and API keys appear here."
            }
            action={
              narrowed ? (
                <Button
                  outline
                  onClick={() =>
                    go({
                      types: [],
                      actor: "",
                      project: "",
                      q: "",
                      window: defaultEventWindow,
                      from: "",
                      to: "",
                    })
                  }
                >
                  Clear filters
                </Button>
              ) : undefined
            }
          />
        ) : null}
      </div>
      <EventFiltersDialog
        open={dialog === "filters"}
        onClose={() => setDialog(null)}
        value={{
          types: query.types,
          actor: query.actor,
          project: query.project,
          window: query.window,
          from: query.from,
          to: query.to,
        }}
        choices={namedChoices(choices, refs)}
        onApply={apply}
      />
    </>
  );
}
// The state with one applied filter taken away.
function without(id: string): Partial<EventQuery> {
  if (id === "types") return { types: [] };
  if (id === "actor") return { actor: "" };
  if (id === "project") return { project: "" };
  if (id === "date") return { window: defaultEventWindow, from: "", to: "" };
  return { q: "" };
}
// One badge per applied filter, in the dialog's order. The date is always
// shown, so the filter kept inside the dialog stays visible.
function appliedBadges(
  query: EventQuery,
  refs: EventPage["refs"] | undefined,
): FilterBadge[] {
  const badges: FilterBadge[] = [];
  if (query.types.length > 0) {
    const names = query.types.map(
      (t) => eventTypes.find((g) => g.id === t)?.label ?? eventLabels[t] ?? t,
    );
    badges.push({
      id: "types",
      text: `Type: ${names.slice(0, 2).join(", ")}${names.length > 2 ? ` +${names.length - 2}` : ""}`,
      tooltip: names.length > 2 ? names.join(", ") : undefined,
    });
  }
  if (query.actor)
    badges.push({
      id: "actor",
      text: `Person: ${refs?.actor?.id === query.actor && refs.actor.name ? refs.actor.name : "A person"}`,
    });
  if (query.project)
    badges.push({
      id: "project",
      text: `Project: ${refs?.project?.id === query.project && refs.project.name ? refs.project.name : query.project}`,
    });
  badges.push({ id: "date", text: `Date: ${dateLabel(query)}` });
  if (query.q) badges.push({ id: "q", text: `Search: ${query.q}` });
  return badges;
}
// The pickers' lists with the applied person named from the response, for a
// former member the members list no longer holds.
function namedChoices(
  choices: FilterChoices,
  refs: EventPage["refs"] | undefined,
): FilterChoices {
  const ref = refs?.actor;
  if (!choices.people || !ref?.name) return choices;
  return {
    ...choices,
    people: choices.people.some((p) => p.id === ref.id)
      ? choices.people
      : [...choices.people, ref],
  };
}
function EventTable({
  label,
  events,
  links,
}: {
  label: string;
  events: WorkspaceEvent[];
  links: EventLinks | null;
}) {
  return (
    <Table dense aria-label={label}>
      <TableHead>
        <TableRow>
          <TableHeader className="w-0">
            <span className="sr-only">Details</span>
          </TableHeader>
          <TableHeader className="w-0 whitespace-nowrap">
            Time (UTC)
          </TableHeader>
          <TableHeader className="w-full">Change</TableHeader>
        </TableRow>
      </TableHead>
      <TableBody>
        {events.map((event) => (
          <EventRow key={event.id} event={event} links={links} />
        ))}
      </TableBody>
    </Table>
  );
}
// A change in one line, opening in place to its details (the list already
// holds them: nothing more is loaded), and its time links to its page.
function EventRow({
  event,
  links,
}: {
  event: WorkspaceEvent;
  links: EventLinks | null;
}) {
  const detailsId = useId();
  const [open, setOpen] = useState(false);
  const sentence = eventSentence(event);
  const summary = eventSummary(event);
  const who = whoIs(event.actor, event.apiKey);
  const page = links?.events
    ? routes.events.show(links.workspace, event.id)
    : null;
  return (
    <Fragment>
      <TableRow className={cx(open && "*:border-b-0")}>
        <TableCell className="w-0 pr-0">
          <Button
            plain
            aria-expanded={open}
            aria-controls={detailsId}
            aria-label={`Details: ${who} ${sentence}`}
            onClick={() => setOpen(!open)}
            className="-my-1.5"
          >
            <ChevronRightIcon
              className={cx(
                "transition-transform motion-reduce:transition-none",
                open && "rotate-90",
              )}
            />
          </Button>
        </TableCell>
        <TableCell className="whitespace-nowrap align-middle tabular-nums">
          {page ? (
            <TextLink href={page}>{shortTime(event.createdAt)}</TextLink>
          ) : (
            shortTime(event.createdAt)
          )}
        </TableCell>
        <TableCell className="w-full whitespace-normal">
          <div className="line-clamp-2 wrap-anywhere">
            {event.actor ? (
              <MemberLink
                workspace={links?.members ? links.workspace : null}
                member={event.actor}
                className="font-medium"
              />
            ) : (
              <span className="font-medium">{who}</span>
            )}{" "}
            {sentence}
            {summary && (
              <span className="block text-zinc-500 dark:text-zinc-400">
                {summary}
              </span>
            )}
          </div>
        </TableCell>
      </TableRow>
      {open && (
        <TableRow id={detailsId}>
          <TableCell colSpan={3} className="whitespace-normal">
            <EventDetails event={event} links={links} />
          </TableCell>
        </TableRow>
      )}
    </Fragment>
  );
}
// "Oct 31, 2026, 2:05 PM" without its zone: the column says UTC.
function shortTime(value: string) {
  return formatDateTime(value).replace(/ UTC$/, "");
}
