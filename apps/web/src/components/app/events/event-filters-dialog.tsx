import { useMemo, useState, type FormEvent } from "react";
import { Combobox, type ComboboxNode } from "@/components/ui/combobox";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, Label } from "@/components/ui/fieldset";
import { Button } from "@/components/ui/button";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { eventLabels } from "@/lib/event-log";
import { useOpenCount } from "@/lib/use-open-count";
import { eventTypes, type EventQuery, type EventRef } from "@/types/events";
import { compressTypes, expandTypes } from "./event-filters";
// The filters of a list of changes (docs/ui/forms-and-actions.md, List
// filters): the types, the person and the project, applied together. The time
// range and the search stay above the list.
export type FilterPatch = Pick<EventQuery, "types" | "actor" | "project">;
// Who and which projects the pickers offer; null where a list has no such
// filter (a key's own log).
export type FilterChoices = {
  people: EventRef[] | null;
  projects: EventRef[] | null;
};
const any = "any";
export function EventFiltersDialog({
  open,
  onClose,
  value,
  choices,
  onApply,
}: {
  open: boolean;
  onClose: () => void;
  value: FilterPatch;
  choices: FilterChoices;
  onApply: (patch: FilterPatch) => void;
}) {
  const opened = useOpenCount(open);
  return (
    <Dialog open={open} onClose={onClose} size="xl">
      <FiltersForm
        key={opened}
        value={value}
        choices={choices}
        onApply={onApply}
        onCancel={onClose}
      />
    </Dialog>
  );
}
// The types as the picker's tree: each group, then its events.
const typeOptions: ComboboxNode[] = eventTypes.map((group) => ({
  value: group.id,
  text: group.label,
  chip: group.label,
  label: <span className="font-medium">{group.label}</span>,
  children: group.names.map((name) => ({
    value: name,
    text: eventLabels[name] ?? name,
    label: eventLabels[name] ?? name,
  })),
}));
function FiltersForm({
  value,
  choices,
  onApply,
  onCancel,
}: {
  value: FilterPatch;
  choices: FilterChoices;
  onApply: (patch: FilterPatch) => void;
  onCancel: () => void;
}) {
  const [picked, setPicked] = useState(() => expandTypes(value.types));
  const [actor, setActor] = useState(value.actor || any);
  const [project, setProject] = useState(value.project || any);
  // The pickers always hold what is applied, even when it is no longer in
  // their list (a former member, a deleted project).
  const people = useMemo(
    () => withApplied(choices.people, value.actor),
    [choices.people, value.actor],
  );
  const projects = useMemo(
    () => withApplied(choices.projects, value.project),
    [choices.projects, value.project],
  );
  const submit = (e: FormEvent) => {
    e.preventDefault();
    onApply({
      types: compressTypes(picked),
      actor: actor === any ? "" : actor,
      project: project === any ? "" : project,
    });
  };
  return (
    <form onSubmit={submit} aria-label="Filter changes">
      <DialogTitle>Filter changes</DialogTitle>
      <DialogDescription>
        Filters apply together. The period and search stay above the list.
      </DialogDescription>
      <DialogBody className="space-y-6">
        <Field>
          <Label>Type</Label>
          <Combobox
            value={picked}
            onChange={setPicked}
            options={typeOptions}
            placeholder="All types"
          />
        </Field>
        {people && (
          <Field>
            <Label>Person</Label>
            <Listbox value={actor} onChange={setActor}>
              <ListboxOption value={any}>
                <ListboxLabel>Everyone</ListboxLabel>
              </ListboxOption>
              {people.map((person) => (
                <ListboxOption key={person.id} value={person.id}>
                  <ListboxLabel>
                    {person.name || "A deleted account"}
                  </ListboxLabel>
                </ListboxOption>
              ))}
            </Listbox>
          </Field>
        )}
        {projects && (
          <Field>
            <Label>Project</Label>
            <Listbox value={project} onChange={setProject}>
              <ListboxOption value={any}>
                <ListboxLabel>Any project</ListboxLabel>
              </ListboxOption>
              {projects.map((p) => (
                <ListboxOption key={p.id} value={p.id}>
                  <ListboxLabel>{p.name || p.id}</ListboxLabel>
                </ListboxOption>
              ))}
            </Listbox>
          </Field>
        )}
      </DialogBody>
      <DialogActions>
        <Button
          plain
          onClick={() => {
            setPicked([]);
            setActor(any);
            setProject(any);
          }}
        >
          Clear all
        </Button>
        <Button plain onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit">Apply filters</Button>
      </DialogActions>
    </form>
  );
}
function withApplied(list: EventRef[] | null, applied: string) {
  if (!list) return null;
  return applied && !list.some((item) => item.id === applied)
    ? [...list, { id: applied }]
    : list;
}
