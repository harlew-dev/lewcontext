import { useCallback, useState, type FormEvent } from "react";
import { ArrowTopRightOnSquareIcon } from "@heroicons/react/16/solid";
import { PasswordField } from "@/components/app/account/password-fields";
import { AppOrRecoveryCode } from "@/components/app/mfa/mfa-setup";
import { Button } from "@/components/ui/button";
import { Combobox, type ComboboxNode } from "@/components/ui/combobox";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
import { CopyButton, CopyFailure, useCopy } from "@/components/ui/copy-button";
import {
  Dialog,
  DialogTitle,
  DialogBody,
  DialogActions,
  DialogDescription,
} from "@/components/ui/dialog";
import { Disclosure } from "@/components/ui/disclosure";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import {
  Field,
  FieldGroup,
  Fieldset,
  Label,
  Description,
  ErrorMessage,
} from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";
import { Listbox, ListboxOption, ListboxLabel } from "@/components/ui/listbox";
import { Reveal } from "@/components/ui/reveal";
import { Text, TextLink } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { ApiError, paths, useApi } from "@/lib/api";
import { routes } from "@/lib/routes";
import { useSession } from "@/lib/session";
import { useAction } from "@/lib/use-action";
import { useResource } from "@/lib/use-resource";
import { useResourcePages } from "@/lib/use-resource-pages";
import { useWorkspace, useWorkspaceList } from "@/lib/workspace";
import type { ApiKey, ApiKeyInput } from "@/types/api-keys";
import { can, type Workspace } from "@/types/workspaces";

type Props = {
  workspace?: Workspace;
  editing?: ApiKey;
  onClose: () => void;
  onSaved: () => void;
};

// Only this mounted creation flow holds a plaintext credential. Its parent
// unmounts on navigation/workspace changes; no SWR mutation ever receives it.
export function KeyDialog({ workspace, editing, onClose, onSaved }: Props) {
  const api = useApi();
  const session = useSession();
  const current = useWorkspace();
  const [workspaceId, setWorkspaceId] = useState(
    editing?.workspace.id ?? workspace?.id ?? current.id,
  );
  const [name, setName] = useState(editing?.name ?? "");
  const [access, setAccess] = useState<"all" | "selected">(
    editing?.projectAccess ?? "all",
  );
  const [projects, setProjects] = useState(
    editing?.projects.map((p) => p.id) ?? [],
  );
  const [actions, setActions] = useState(() => {
    if (!editing) return ["read"];
    return ["read", "store", "update", "destroy"].filter((a) =>
      a === "read"
        ? editing.scopes.includes("project.index") ||
          editing.scopes.includes("project.show")
        : editing.scopes.includes(`project.${a}`),
    );
  });
  const [expiry, setExpiry] = useState(editing ? "keep" : "90");
  const [openedAt] = useState(() => Date.now());
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [authenticating, setAuthenticating] = useState(false);
  const [secret, setSecret] = useState("");
  const [discard, setDiscard] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const scopes = actions.flatMap((a) =>
    a === "read" ? ["project.index", "project.show"] : [`project.${a}`],
  );
  const needsProof =
    !editing ||
    (editing.projectAccess === "selected" && access === "all") ||
    scopes.some((s) => !editing.scopes.includes(s)) ||
    (editing.projectAccess === "selected" &&
      access === "selected" &&
      projects.some((id) => !editing.projects.some((p) => p.id === id)));
  const selected = useResource(
    workspace ? null : paths.workspaces.show(workspaceId),
  );
  const chosen =
    workspace ??
    (selected.data?.id === workspaceId ? selected.data : undefined);
  const setProjectAccess = (value: "all" | "selected") => {
    setAccess(value);
    if (value === "selected")
      setActions((previous) => previous.filter((a) => a !== "store"));
  };
  const options: ComboboxNode[] = [
    {
      value: "projects",
      label: "Projects",
      text: "Projects",
      children: [
        { value: "read", label: "Read", text: "Read" },
        ...(["store", "update", "destroy"] as const)
          .filter(
            (action) =>
              chosen &&
              (workspace || can(chosen, `project.${action}`)) &&
              (action !== "store" || access === "all"),
          )
          .map((action) => ({
            value: action,
            text: { store: "Create", update: "Update", destroy: "Delete" }[
              action
            ],
            label: { store: "Create", update: "Update", destroy: "Delete" }[
              action
            ],
          })),
      ],
    },
  ];
  const initialProjects = editing?.projects ?? [];
  const save = useAction(
    async (input: ApiKeyInput) => {
      const client = api.apiKeys(workspace?.id);
      if (editing) {
        await client.update(editing.id, input);
        return "";
      }
      const result = await client.store({
        ...input,
        ...(!workspace ? { workspace: workspaceId } : {}),
      });
      return result.secret;
    },
    {
      onError(error) {
        setUncertain(!(error instanceof ApiError) || error.status >= 500);
        setFieldErrors(
          error instanceof ApiError && error.field
            ? { [error.field]: error.message }
            : {},
        );
      },
    },
  );
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (save.busy) return;
    if (needsProof && session.user.mfaEnabledAt && !authenticating) {
      setCode("");
      setFieldErrors({});
      setAuthenticating(true);
      return;
    }
    const expiresAt =
      expiry === "keep"
        ? editing!.expiresAt
        : expiry === "never"
          ? null
          : new Date(Date.now() + Number(expiry) * 86400000).toISOString();
    const result = await save.run({
      name: name.trim(),
      projectAccess: access,
      projects: access === "all" ? [] : projects,
      scopes,
      expiresAt,
      ...(password ? { password } : {}),
      ...(code ? { code } : {}),
    });
    setPassword("");
    setCode("");
    if (!result.ok) return;
    setAuthenticating(false);
    onSaved();
    toast.success(editing ? "API key updated." : "API key created.");
    if (editing) onClose();
    else setSecret(result.value);
  };
  const close = () => {
    if (save.busy) return;
    if (secret) setDiscard(true);
    else onClose();
  };
  return (
    <>
      <Dialog open onClose={close} size="xl">
        <DialogTitle>
          {secret ? "Your API key" : editing ? "Edit API key" : "Add API Key"}
        </DialogTitle>
        <DialogDescription>
          {secret
            ? "Copy this key now. You won't be able to see it again."
            : "API keys are separate long-lived credentials. API calls do not prompt for MFA."}
        </DialogDescription>
        {secret ? (
          <>
            <DialogBody>
              <SecretStep secret={secret} workspace={workspaceId} />
            </DialogBody>
            <DialogActions>
              <Button
                onClick={() => {
                  setSecret("");
                  onClose();
                }}
              >
                Done
              </Button>
            </DialogActions>
          </>
        ) : (
          <form onSubmit={submit}>
            <DialogBody>
              <Fieldset disabled={save.busy}>
                <FieldGroup>
                  <Field>
                    <Label>Name</Label>
                    <Input
                      value={name}
                      onChange={(e) => setName(e.currentTarget.value)}
                      maxLength={80}
                      required
                      autoFocus
                      invalid={Boolean(fieldErrors.name)}
                    />
                    {fieldErrors.name && (
                      <ErrorMessage>{fieldErrors.name}</ErrorMessage>
                    )}
                  </Field>
                  {!workspace && !editing && (
                    <WorkspaceField
                      value={workspaceId}
                      onChange={(id) => {
                        setWorkspaceId(id);
                        setProjects([]);
                        setActions(["read"]);
                      }}
                    />
                  )}
                  {selected.error && (
                    <Notice variant="error" onRetry={selected.refresh}>
                      {selected.error}
                    </Notice>
                  )}
                  <Field>
                    <Label>Expiry</Label>
                    <Listbox value={expiry} onChange={setExpiry}>
                      {editing && (
                        <ListboxOption value="keep">
                          <ListboxLabel>
                            Keep current expiry
                            {editing.expiresAt ? "" : " (Never)"}
                          </ListboxLabel>
                        </ListboxOption>
                      )}
                      {[1, 7, 30, 90, 365]
                        .filter(
                          (days) =>
                            !editing?.expiresAt ||
                            openedAt + days * 86400000 <
                              Date.parse(editing.expiresAt),
                        )
                        .map((days) => (
                          <ListboxOption key={days} value={String(days)}>
                            <ListboxLabel>
                              {days} {days === 1 ? "day" : "days"}
                            </ListboxLabel>
                          </ListboxOption>
                        ))}
                      {(!editing || !editing.expiresAt) && (
                        <ListboxOption value="never">
                          <ListboxLabel>Never</ListboxLabel>
                        </ListboxOption>
                      )}
                    </Listbox>
                    {fieldErrors.expiresAt && (
                      <ErrorMessage>{fieldErrors.expiresAt}</ErrorMessage>
                    )}
                  </Field>
                  <Field>
                    <Label>Project access</Label>
                    <Listbox value={access} onChange={setProjectAccess}>
                      <ListboxOption value="all">
                        <ListboxLabel>All projects</ListboxLabel>
                      </ListboxOption>
                      <ListboxOption value="selected">
                        <ListboxLabel>Selected projects</ListboxLabel>
                      </ListboxOption>
                    </Listbox>
                    <Description>
                      All projects includes existing and future projects in this
                      workspace.
                    </Description>
                  </Field>
                  {access === "selected" && (
                    <ProjectPicker
                      key={workspaceId}
                      workspace={workspaceId}
                      value={projects}
                      onChange={setProjects}
                      initial={initialProjects}
                    />
                  )}
                  <Field>
                    <Label>Permissions</Label>
                    <Combobox
                      value={actions}
                      onChange={setActions}
                      options={options}
                      placeholder="Choose permissions"
                      disabled={!chosen}
                      invalid={Boolean(fieldErrors.scopes)}
                    />
                    <Description>
                      {access === "selected"
                        ? "Selected-project keys cannot create projects. "
                        : ""}
                      {!workspace &&
                        "Permissions are also limited by your current workspace role."}
                    </Description>
                    {fieldErrors.scopes && (
                      <ErrorMessage>{fieldErrors.scopes}</ErrorMessage>
                    )}
                  </Field>
                  {needsProof && !session.user.mfaEnabledAt && (
                    <PasswordField
                      label="Current password"
                      autoComplete="current-password"
                      value={password}
                      onChange={setPassword}
                      error={fieldErrors.password}
                      disabled={save.busy}
                      autoFocus={false}
                    />
                  )}
                  {editing && (
                    <Text>
                      Fresh account proof is needed only when increasing access;
                      it can be left blank for other changes.
                    </Text>
                  )}
                </FieldGroup>
              </Fieldset>
              <ErrorNotice error={save.error} />
              {uncertain && !editing && (
                <Notice variant="warning">
                  If the response was lost, the key may have been created. Check
                  the list and revoke that record before creating another. Its
                  secret cannot be recovered.
                </Notice>
              )}
            </DialogBody>
            <DialogActions>
              <Button plain onClick={close} disabled={save.busy}>
                Cancel
              </Button>
              <Button
                type="submit"
                loading={save.loading}
                disabled={
                  !name.trim() ||
                  actions.length === 0 ||
                  !chosen ||
                  (access === "selected" &&
                    (projects.length === 0 || projects.length > 100))
                }
                disabledReason="Fill in required fields"
              >
                {editing ? "Save changes" : "Add API Key"}
              </Button>
            </DialogActions>
          </form>
        )}
        {authenticating && (
          <Dialog
            open
            onClose={() => {
              if (save.busy) return;
              setCode("");
              setAuthenticating(false);
            }}
            size="md"
          >
            <form onSubmit={submit}>
              <DialogTitle>Confirm your identity</DialogTitle>
              <DialogDescription>
                Enter an authenticator or recovery code to{" "}
                {editing ? "increase this key’s access" : "create this API key"}
                .
              </DialogDescription>
              <DialogBody>
                <AppOrRecoveryCode
                  autoFocus
                  value={code}
                  onChange={setCode}
                  error={fieldErrors.code}
                  disabled={save.loading}
                />
                <ErrorNotice error={save.error} />
                {uncertain && !editing && (
                  <Notice variant="warning">
                    The key may have been created. Check the list before trying
                    again; its secret cannot be recovered.
                  </Notice>
                )}
              </DialogBody>
              <DialogActions>
                <Button
                  plain
                  disabled={save.loading}
                  onClick={() => {
                    if (!save.busy) {
                      setCode("");
                      setAuthenticating(false);
                    }
                  }}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  loading={save.loading}
                  disabled={!code.trim()}
                  disabledReason="Fill in required fields"
                >
                  {editing ? "Save changes" : "Add API Key"}
                </Button>
              </DialogActions>
            </form>
          </Dialog>
        )}
      </Dialog>
      <ConfirmationDialog
        open={discard}
        onClose={() => setDiscard(false)}
        title="Discard this key's secret?"
        description="You won't be able to see it again. The key stays active; revoke it from the list if it wasn't saved."
        confirm="Discard secret"
        onConfirm={async () => {
          setSecret("");
        }}
        onConfirmed={onClose}
      />
    </>
  );
}

function WorkspaceField({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const list = useWorkspaceList();
  return (
    <div className="space-y-3">
      <Field>
        <Label>Workspace</Label>
        <Listbox value={value} onChange={onChange}>
          {list.items.map((w) => (
            <ListboxOption key={w.id} value={w.id}>
              <ListboxLabel>{w.name}</ListboxLabel>
            </ListboxOption>
          ))}
        </Listbox>
      </Field>
      {list.error && (
        <Notice variant="error" onRetry={list.retry}>
          {list.error}
        </Notice>
      )}
      {list.hasMore && (
        <div className="flex justify-end">
          <Button outline loading={list.loadingMore} onClick={list.loadMore}>
            Show more workspaces
          </Button>
        </div>
      )}
    </div>
  );
}

function ProjectPicker({
  workspace,
  value,
  onChange,
  initial,
}: {
  workspace: string;
  value: string[];
  onChange: (value: string[]) => void;
  initial: { id: string; name: string }[];
}) {
  const [search, setSearch] = useState("");
  const [selectedProjects, setSelectedProjects] = useState(initial);
  const pathFor = useCallback(
    (page: number) => paths.projects(workspace).index({ page, q: search }),
    [workspace, search],
  );
  const list = useResourcePages(pathFor);
  return (
    <div className="space-y-3">
      <Field>
        <Label>Projects</Label>
        <Combobox
          value={value}
          onChange={(ids) => {
            setSelectedProjects(
              ids.map(
                (id) =>
                  list.items.find((p) => p.id === id) ??
                  selectedProjects.find((p) => p.id === id) ?? { id, name: id },
              ),
            );
            onChange(ids);
          }}
          options={list.items.map((p) => ({
            value: p.id,
            label: p.name,
            text: p.name,
          }))}
          selectedOptions={selectedProjects.map((p) => ({
            value: p.id,
            label: p.name,
            text: p.name,
          }))}
          remote={{
            query: search,
            onSearch: setSearch,
            loading: list.loading,
            loadingMore: list.loadingMore,
            hasMore: list.hasMore,
            loadMore: list.loadMore,
            error: list.error,
            retry: list.retry,
          }}
          placeholder="Choose up to 100 projects"
        />
        <Description>{value.length} selected (maximum 100).</Description>
      </Field>
    </div>
  );
}

function SecretStep({
  secret,
  workspace,
}: {
  secret: string;
  workspace: string;
}) {
  const copy = useCopy(secret);
  return (
    <div className="space-y-5">
      <div className="relative rounded-lg bg-zinc-100 p-4 dark:bg-zinc-800">
        <div className="absolute top-2 right-2">
          <CopyButton copy={copy} label="Copy API key" variant="plain" />
        </div>
        <div className="pr-10">
          <Reveal
            label="Reveal API key"
            hideLabel="Hide API key"
            announcement="API key shown."
            placeholder={
              <div className="break-all font-mono">
                lc_wk_apk_00000000000000_•••••••••••••••••••••••••••••••••••••••••••
              </div>
            }
          >
            <code className="block break-all select-all">{secret}</code>
          </Reveal>
        </div>
        <CopyFailure copy={copy}>
          Reveal the key, select it, and copy it manually instead.
        </CopyFailure>
      </div>
      <Disclosure title="Example request">
        <pre className="overflow-x-auto rounded-lg bg-zinc-100 p-4 text-sm whitespace-pre-wrap dark:bg-zinc-800">{`curl '${window.location.origin}/api/v1/workspaces/${workspace}/projects' \\\n  -H "Authorization: Bearer $LEWCONTEXT_API_KEY"`}</pre>
      </Disclosure>
      <Text>
        <TextLink
          href={routes.docs}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1"
        >
          API Documentation
          <ArrowTopRightOnSquareIcon
            aria-hidden="true"
            className="size-4 shrink-0"
          />
        </TextLink>
      </Text>
    </div>
  );
}
