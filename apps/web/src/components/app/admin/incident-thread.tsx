import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { EllipsisHorizontalIcon } from "@heroicons/react/20/solid";
import { EyeSlashIcon } from "@heroicons/react/16/solid";
import { AdminActionDialog } from "./admin-action-dialog";
import { formatEventTime } from "./event-filters";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
import {
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
} from "@/components/ui/dropdown";
import {
  EmptyState,
  ErrorNotice,
  ListSkeleton,
  Notice,
} from "@/components/ui/feedback";
import { Description, Field, Label } from "@/components/ui/fieldset";
import { Subheading } from "@/components/ui/heading";
import { Listbox, ListboxOption } from "@/components/ui/listbox";
import { Textarea } from "@/components/ui/textarea";
import { Text } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { adminCan, useAdminKey, useAdminSession } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { ApiError } from "@/lib/api";
import { adminPaths, useAdminApi, type IncidentKind } from "@/lib/api/admin";
import { useAction } from "@/lib/use-action";
import { useAdminResource } from "@/lib/use-admin-resource";
import { useOpenCount } from "@/lib/use-open-count";
import type {
  IncidentCycle,
  IncidentMessage,
  IncidentThreadData,
} from "@/types/admin";

type ThreadProps = {
  kind: IncidentKind;
  parent: string;
  thread: IncidentThreadData;
  canResolve: boolean;
  resolveReason?: string;
  onResolve: (cycle: string, message: string) => Promise<unknown>;
  onChanged: () => void;
};

// Shared alert/error discussion. Each selection is an explicit audited read;
// drafts stay in component memory, and conflicts never retarget a decision.
export function IncidentThread(props: ThreadProps) {
  const { kind, parent, thread } = props;
  const api = useAdminApi();
  const [selected, setSelected] = useState<string | null>(null);
  const [olderCycles, setOlderCycles] = useState<IncidentCycle[]>([]);
  const [next, setNext] = useState<string | null | undefined>(undefined);
  const loadOlder = useAction(async () => {
    const before = next === undefined ? thread.nextCycle : next;
    if (!before) return;
    const page = await api.incidentCycles(kind, parent).index({ before });
    setOlderCycles((old) => [...old, ...page.data]);
    setNext(page.next);
  });
  const cycles = [
    ...(thread.cycles ?? []),
    ...olderCycles.filter(
      (c) => !thread.cycles?.some((known) => known.id === c.id),
    ),
  ];
  const cycle = cycles.find((c) => c.id === selected) ?? thread.currentCycle;
  if (!cycle?.id) return null;
  return (
    <section className="mt-10 space-y-5" aria-label="Incident discussion">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <Subheading>Discussion</Subheading>
        <Listbox
          aria-label="Incident cycle"
          value={cycle.id}
          onChange={setSelected}
        >
          {cycles.map((c) => (
            <ListboxOption key={c.id} value={c.id}>
              Cycle {c.number}
              {c.resolvedAt ? " · Resolved" : " · Open"}
            </ListboxOption>
          ))}
        </Listbox>
      </div>
      <CycleSummary cycle={cycle} />
      {(next === undefined ? thread.nextCycle : next) && (
        <div className="flex justify-end">
          <Button
            outline
            loading={loadOlder.loading}
            onClick={() => void loadOlder.run()}
          >
            Load older cycles
          </Button>
        </div>
      )}
      <ErrorNotice error={loadOlder.error} />
      <CycleMessages key={cycle.id} {...props} cycle={cycle} />
    </section>
  );
}

function CycleSummary({ cycle }: { cycle: IncidentCycle }) {
  const source =
    cycle.resolutionSource === "automatic_legacy"
      ? "Automatic (legacy)"
      : cycle.resolutionSource === "unknown_legacy"
        ? "Unknown (legacy)"
        : (cycle.resolver?.name ?? "Deleted account");
  return (
    <Text>
      Cycle {cycle.number} opened {formatEventTime(cycle.openedAt)} UTC.
      {cycle.resolvedAt && (
        <>
          {" "}
          Resolved by {source} at {formatEventTime(cycle.resolvedAt)} UTC.
        </>
      )}
    </Text>
  );
}

function CycleMessages({
  cycle,
  ...props
}: ThreadProps & { cycle: IncidentCycle }) {
  const {
    kind,
    parent,
    thread,
    canResolve,
    onResolve,
    onChanged,
    resolveReason,
  } = props;
  const api = useAdminApi();
  const navigate = useNavigate();
  const key = useAdminKey();
  const operator = adminCan(useAdminSession().role, "operator");
  const [before, setBefore] = useState<string | undefined>();
  const [previous, setPrevious] = useState<(string | undefined)[]>([]);
  const messages = useAdminResource(
    adminPaths
      .incidentMessages(kind, parent)
      .index({ cycle: cycle.id, before }),
  );
  const [resolution, setResolution] = useState<{
    message: IncidentMessage;
    run: () => Promise<unknown>;
  } | null>(null);
  const [redaction, setRedaction] = useState<IncidentMessage | null>(null);
  const resolutionOpened = useOpenCount(resolution !== null);
  const redactionOpened = useOpenCount(redaction !== null);
  const current =
    cycle.id === thread.currentCycle.id && cycle.resolvedAt === null;
  function changed() {
    messages.refresh();
    onChanged();
  }
  function failed(error: unknown) {
    if (error instanceof ApiError && error.status === 401) {
      void navigate(adminRoutes.login(key), { replace: true });
      return true;
    }
    if (error instanceof ApiError && error.status === 409) changed();
    return false;
  }
  return (
    <div className="space-y-5">
      {operator && current && !canResolve && (
        <Text>
          {resolveReason ??
            "This incident cannot be resolved in its current state."}
        </Text>
      )}
      {messages.error && (
        <Notice variant="error" onRetry={messages.refresh}>
          {messages.error}
        </Notice>
      )}
      {!messages.data ? (
        messages.loading && <ListSkeleton label="Loading discussion" />
      ) : (
        <>
          <div aria-busy={messages.refreshing} className="space-y-4">
            {messages.data.data.length === 0 && (
              <EmptyState
                title="No comments yet"
                description="Share what you investigated and what should happen next."
              />
            )}
            {[...messages.data.data].reverse().map((message) => (
              <article
                key={message.id}
                className="rounded-lg border border-zinc-950/10 p-4 dark:border-white/10"
              >
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <Text>
                    {message.author?.name ?? "Deleted account"} ·{" "}
                    {formatEventTime(message.createdAt)} UTC
                  </Text>
                  {operator && message.body !== null && (
                    <Dropdown>
                      <DropdownButton plain aria-label="Comment actions">
                        <EllipsisHorizontalIcon data-slot="icon" />
                      </DropdownButton>
                      <DropdownMenu>
                        <DropdownItem
                          variant="danger"
                          onClick={() => setRedaction(message)}
                        >
                          <EyeSlashIcon />
                          Redact comment
                        </DropdownItem>
                      </DropdownMenu>
                    </Dropdown>
                  )}
                </div>
                {cycle.resolutionMessage === message.id && (
                  <Badge color="lime">Resolved incident</Badge>
                )}
                <p className="mt-3 text-base/7 whitespace-pre-wrap break-words text-zinc-950 sm:text-sm/6 dark:text-white">
                  {message.body ??
                    (cycle.resolutionMessage === message.id
                      ? "Resolution comment redacted"
                      : "Comment redacted")}
                </p>
                {message.redactedAt && (
                  <Text className="mt-2">
                    Redacted by {message.redactor?.name ?? "Deleted account"} ·{" "}
                    {formatEventTime(message.redactedAt)} UTC
                  </Text>
                )}
                {operator && current && message.body !== null && (
                  <div className="mt-4 flex justify-end">
                    <Button
                      outline
                      disabled={!canResolve}
                      disabledReason={resolveReason}
                      onClick={() =>
                        setResolution({
                          message,
                          run: () => onResolve(cycle.id, message.id),
                        })
                      }
                    >
                      Mark as resolved
                    </Button>
                  </div>
                )}
              </article>
            ))}
          </div>
          <div className="flex justify-end gap-3">
            {previous.length > 0 && (
              <Button
                outline
                onClick={() => {
                  setBefore(previous[previous.length - 1]);
                  setPrevious((old) => old.slice(0, -1));
                }}
              >
                Newer comments
              </Button>
            )}
            {messages.data.next && (
              <Button
                outline
                onClick={() => {
                  setPrevious((old) => [...old, before]);
                  setBefore(messages.data!.next!);
                }}
              >
                Older comments
              </Button>
            )}
          </div>
        </>
      )}
      <Composer
        kind={kind}
        parent={parent}
        cycle={cycle.id}
        onChanged={changed}
        onError={failed}
      />
      <ConfirmationDialog
        key={resolutionOpened}
        open={resolution !== null}
        onClose={() => setResolution(null)}
        title="Resolve this incident?"
        description="This comment will be the resolution decision for this cycle. Its author remains credited separately from you as resolver."
        confirm="Mark as resolved"
        onConfirm={() => resolution!.run()}
        onError={failed}
        onConfirmed={() => {
          toast.success("Incident marked as resolved.");
          changed();
        }}
      />
      <AdminActionDialog
        key={redactionOpened}
        open={redaction !== null}
        onClose={() => setRedaction(null)}
        title="Redact this comment?"
        description="The text is permanently removed and a tombstone remains. Do not repeat sensitive material in the audit reason."
        confirm="Redact comment"
        variant="danger"
        action={(reason) =>
          api
            .incidentMessageRedaction(kind, parent, redaction!.id)
            .store(reason)
        }
        onError={failed}
        onDone={() => {
          toast.success("Comment redacted.");
          changed();
        }}
      />
    </div>
  );
}

function Composer({
  kind,
  parent,
  cycle,
  onChanged,
  onError,
}: {
  kind: IncidentKind;
  parent: string;
  cycle: string;
  onChanged: () => void;
  onError: (error: unknown) => boolean;
}) {
  const api = useAdminApi();
  const [draft, setDraft] = useState("");
  const length = Array.from(draft.trim()).length;
  const post = useAction(
    (body: string) => api.incidentMessages(kind, parent).store({ cycle, body }),
    { onError },
  );
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (length < 1 || length > 2000) return;
    if (!(await post.run(draft.trim())).ok) return;
    setDraft("");
    toast.success("Comment posted.");
    onChanged();
  }
  return (
    <form className="space-y-4" onSubmit={(event) => void submit(event)}>
      <Field disabled={post.loading}>
        <Label>Comment</Label>
        <Description>
          Plain text, 1 to 2,000 characters. Corrections are follow-up comments.
        </Description>
        <Textarea
          rows={4}
          value={draft}
          onChange={(event) => setDraft(event.currentTarget.value)}
        />
        <Text className="mt-1 text-right tabular-nums">
          {length.toLocaleString("en-US")} / 2,000
        </Text>
      </Field>
      <div className="flex justify-end">
        <Button
          type="submit"
          loading={post.loading}
          disabled={length < 1 || length > 2000}
          disabledReason="Fill in required fields"
        >
          Post comment
        </Button>
      </div>
      <ErrorNotice error={post.error} />
    </form>
  );
}
