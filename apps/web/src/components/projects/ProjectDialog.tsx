import { useState } from "react";
import { Modal, ModalHeader, ModalBody, ModalFooter } from "baseui/modal";
import { Select } from "baseui/select";
import { FormControl } from "baseui/form-control";
import { Plus, Check } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { Field, TextAreaField } from "@/components/ui/Field";
import { ErrorNotice } from "@/components/ui/Feedback";
import { request, errorMessage } from "@/lib/api";
import { apiRoutes } from "@/lib/routes";
import { statuses, colors, type Project, type ProjectInput } from "@/lib/types";
export function ProjectDialog({
  project,
  csrfToken,
  onClose,
  onSaved,
}: {
  project?: Project;
  csrfToken: string;
  onClose: () => void;
  onSaved: (project: Project) => Promise<void>;
}) {
  const [form, setForm] = useState<ProjectInput>(
    project || {
      name: "",
      description: "",
      status: "Planned",
      color: "violet",
    },
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      const result = await request<Project>(
        project ? `${apiRoutes.projects}/${project.id}` : apiRoutes.projects,
        {
          method: project ? "PATCH" : "POST",
          body: JSON.stringify({
            name: form.name,
            description: form.description,
            status: form.status,
            color: form.color,
          }),
        },
        csrfToken,
      );
      await onSaved(result);
      onClose();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }
  return (
    <Modal
      isOpen
      onClose={saving ? undefined : onClose}
      closeable={!saving}
      animate
      role="dialog"
      name={project ? "Edit project" : "Create project"}
    >
      <form onSubmit={submit}>
        <ModalHeader>
          {project ? "A little fine-tuning." : "Make something happen."}
        </ModalHeader>
        <ModalBody>
          <p className="modal-description">
            {project
              ? "Update the details and keep your project moving."
              : "Every good idea starts somewhere. Give yours a home."}
          </p>
          {error && <ErrorNotice message={error} />}
          <Field
            label="Project name"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.currentTarget.value })}
            placeholder="e.g. Website redesign"
            required
            maxLength={100}
            autoFocus
            disabled={saving}
          />
          <TextAreaField
            disabled={saving}
            label="Description"
            value={form.description}
            onChange={(description) => setForm({ ...form, description })}
          />
          <FormControl label="Status" htmlFor="project-status">
            <Select
              id="project-status"
              aria-label="Project status"
              options={statuses.map((status) => ({
                id: status,
                label: status,
              }))}
              value={[{ id: form.status, label: form.status }]}
              clearable={false}
              searchable
              size="compact"
              disabled={saving}
              onChange={({ value }) =>
                setForm({
                  ...form,
                  status: value[0].id as ProjectInput["status"],
                })
              }
            />
          </FormControl>
          <fieldset className="color-picker">
            <legend>Project color</legend>
            {colors.map((color) => (
              <button
                key={color}
                type="button"
                className={`color-choice color-${color} ${form.color === color ? "selected" : ""}`}
                aria-label={color}
                aria-pressed={form.color === color}
                disabled={saving}
                onClick={() => setForm({ ...form, color })}
              >
                {form.color === color && <Check size={16} />}
              </button>
            ))}
          </fieldset>
        </ModalBody>
        <ModalFooter>
          <Button
            type="button"
            kind="tertiary"
            onClick={onClose}
            disabled={saving}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            isLoading={saving}
            disabled={!form.name.trim()}
            startEnhancer={project ? undefined : () => <Plus size={16} />}
          >
            {project ? "Save changes" : "Create project"}
          </Button>
        </ModalFooter>
      </form>
    </Modal>
  );
}
