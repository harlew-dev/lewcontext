import { useState, type FormEvent } from "react";
import { CheckIcon } from "@heroicons/react/16/solid";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Notice } from "@/components/ui/feedback";
import {
  Field,
  FieldGroup,
  Fieldset,
  Label,
  Legend,
} from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { Textarea } from "@/components/ui/textarea";
import { cx } from "@/lib/cx";
import { useApi } from "@/lib/api";
import { useActionError } from "@/lib/use-action-error";
import { useWorkspace } from "@/lib/workspace";
import {
  statuses,
  colors,
  type Project,
  type ProjectInput,
} from "@/types/projects";
import { projectColors } from "./project-colors";
import type { ProjectEditor } from "./use-project-editor";
const emptyProject: ProjectInput = {
  name: "",
  description: "",
  status: "Planned",
  color: "violet",
};
// Create/edit dialog driven by the page's project editor. The last target is
// kept while the dialog animates out so its content doesn't change mid-exit.
export function ProjectDialog({ editor }: { editor: ProjectEditor }) {
  const [target, setTarget] = useState<Project | "new">("new");
  if (editor.dialog && editor.dialog !== target) setTarget(editor.dialog);
  const key = target === "new" ? "new" : target.id;
  return (
    <ProjectForm
      key={key}
      open={editor.dialog !== null}
      project={target === "new" ? undefined : target}
      onClose={() => editor.setDialog(null)}
      onSaved={editor.saved}
    />
  );
}
function ProjectForm({
  open,
  project,
  onClose,
  onSaved,
}: {
  open: boolean;
  project?: Project;
  onClose: () => void;
  onSaved: (project: Project) => void;
}) {
  const api = useApi();
  const workspace = useWorkspace();
  const [form, setForm] = useState<ProjectInput>(project ?? emptyProject);
  const [saving, setSaving] = useState(false);
  const error = useActionError();
  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    try {
      const projects = api.projects(workspace.id);
      const saved = project
        ? await projects.update(project.id, form)
        : await projects.create(form);
      error.clear();
      onSaved(saved);
      onClose();
    } catch (e) {
      error.fail(e);
    } finally {
      setSaving(false);
    }
  }
  return (
    <Dialog open={open} onClose={() => !saving && onClose()}>
      <form onSubmit={submit}>
        <DialogTitle>{project ? "Edit project" : "New project"}</DialogTitle>
        <DialogDescription>
          {project
            ? "Update the details and keep your project moving."
            : "Every good idea starts somewhere. Give yours a home."}
        </DialogDescription>
        <DialogBody>
          {error.message && (
            <Notice key={error.key} variant="error" className="mb-6">
              {error.message}
            </Notice>
          )}
          <FieldGroup>
            <Field disabled={saving}>
              <Label>Project name</Label>
              <Input
                autoFocus
                required
                maxLength={100}
                placeholder="e.g. Website redesign"
                value={form.name}
                onChange={(e) =>
                  setForm({ ...form, name: e.currentTarget.value })
                }
              />
            </Field>
            <Field disabled={saving}>
              <Label>Description</Label>
              <Textarea
                rows={3}
                maxLength={500}
                placeholder="What are you working toward?"
                value={form.description}
                onChange={(e) =>
                  setForm({ ...form, description: e.currentTarget.value })
                }
              />
            </Field>
            <Field disabled={saving}>
              <Label>Status</Label>
              <Listbox
                value={form.status}
                onChange={(status) => setForm({ ...form, status })}
              >
                {statuses.map((status) => (
                  <ListboxOption key={status} value={status}>
                    <ListboxLabel>{status}</ListboxLabel>
                  </ListboxOption>
                ))}
              </Listbox>
            </Field>
            <Fieldset disabled={saving}>
              <Legend>Color</Legend>
              <div data-slot="control" className="flex gap-3">
                {colors.map((color) => (
                  <ColorSwatch
                    key={color}
                    color={color}
                    selected={form.color === color}
                    onSelect={() => setForm({ ...form, color })}
                  />
                ))}
              </div>
            </Fieldset>
          </FieldGroup>
        </DialogBody>
        <DialogActions>
          <Button plain onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button type="submit" loading={saving} disabled={!form.name.trim()}>
            {project ? "Save changes" : "Create project"}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
function ColorSwatch({
  color,
  selected,
  onSelect,
}: {
  color: Project["color"];
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      aria-label={color}
      aria-pressed={selected}
      onClick={onSelect}
      className={cx(
        "grid size-8 cursor-default place-items-center rounded-full ring-offset-2 ring-offset-white focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-blue-500 disabled:opacity-50 dark:ring-offset-zinc-900",
        projectColors[color],
        selected && "ring-2 ring-zinc-950 dark:ring-white",
      )}
    >
      {selected && <CheckIcon className="size-4" />}
    </button>
  );
}
