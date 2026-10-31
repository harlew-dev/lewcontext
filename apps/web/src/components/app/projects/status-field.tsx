import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { statuses, type StatusFilter } from "@/types/projects";
// Filters a project list by status: All, or one status. Shared by the
// projects page and the admin dashboard's workspace page.
export function StatusField({
  value,
  onChange,
}: {
  value: StatusFilter;
  onChange: (status: StatusFilter) => void;
}) {
  return (
    <Listbox<StatusFilter>
      aria-label="Filter by status"
      value={value}
      onChange={onChange}
    >
      <ListboxOption value="all">
        <ListboxLabel>All</ListboxLabel>
      </ListboxOption>
      {statuses.map((item) => (
        <ListboxOption key={item} value={item}>
          <ListboxLabel>{item}</ListboxLabel>
        </ListboxOption>
      ))}
    </Listbox>
  );
}
