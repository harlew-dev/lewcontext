import {
  createContext,
  useContext,
  useId,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { Select, type Option } from "baseui/select";
import { LayoutGroup, useReducedMotion } from "motion/react";
import {
  popoverExit,
  popoverOverrides,
  slot,
  usePopoverSettled,
} from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { usePopoverScrollLock } from "@/lib/scroll-lock";
import { Badge } from "./badge";
import { useField } from "./fieldset";
import { Highlight } from "./highlight";
import {
  ChevronUpDown,
  ListboxButton,
  ListboxIcons,
  ListboxList,
  ListboxOptionItem,
  ListboxPanel,
  ListboxRoot,
} from "./listbox";
// A searchable multi-select over a tree: groups of options, picked whole or
// one by one. On BaseUI's multi Select (searchable, kept open while
// picking), styled like Listbox. Its value is the leaves picked; choosing a
// group picks every leaf under it, or clears them when all are picked, and
// a partly picked group shows a dash. Search keeps a group while any of its
// leaves match, and lists every leaf of a matching group. Closed, the choice
// shows compressed by group (a fully picked group is one chip), at most
// `maxChips` chips and then "+N", each removable.

// One option: `value` identifies it, `label` is drawn in its row, `text` is
// what search matches and screen readers read, `chip` what a chip says
// (`text` by default). A node with `children` is a group.
export type ComboboxNode = {
  value: string;
  label: ReactNode;
  text: string;
  chip?: string;
  children?: ComboboxNode[];
};
// A row of the flattened tree: its depth, the leaves under it (itself for a
// leaf) and the groups above it.
type Row = Option & {
  id: string;
  text: string;
  node: ComboboxNode;
  depth: number;
  leaves: string[];
  ancestors: string[];
};
// How many chips the closed control shows before "+N".
const maxChips = 2;
// What rows and chips read while BaseUI renders them: the picked leaves and
// the chips.
type ComboboxState = { picked: Set<string>; chips: Row[] };
const StateContext = createContext<ComboboxState>({
  picked: new Set(),
  chips: [],
});
const SettledContext = createContext(false);
// The tree in order, depth first, groups before their children.
function flatten(nodes: ComboboxNode[], depth = 0, ancestors: string[] = []) {
  const rows: Row[] = [];
  for (const node of nodes) {
    const children = flatten(node.children ?? [], depth + 1, [
      ...ancestors,
      node.value,
    ]);
    const leaves = node.children
      ? children.filter((r) => !r.node.children).map((r) => r.id)
      : [node.value];
    rows.push(
      { id: node.value, text: node.text, node, depth, leaves, ancestors },
      ...children,
    );
  }
  return rows;
}
// The picked leaves as chips: a fully picked group is one chip (its
// highest fully picked ancestor), then the leaves left over.
export function compressPicked(
  nodes: ComboboxNode[],
  picked: Set<string>,
): string[] {
  const groups: string[] = [];
  const leaves: string[] = [];
  const walk = (list: ComboboxNode[]) => {
    for (const node of list) {
      if (!node.children) {
        if (picked.has(node.value)) leaves.push(node.value);
        continue;
      }
      const all = flatten([node])[0].leaves;
      if (all.length > 0 && all.every((leaf) => picked.has(leaf)))
        groups.push(node.value);
      else walk(node.children);
    }
  };
  walk(nodes);
  return [...groups, ...leaves];
}
// Whether a row is shown for a search: it matches, a group above it matches
// (a matching group lists everything in it), or, for a group, something in
// it matches (so a match is shown in its group).
function filterRows(rows: Row[], query: string): Row[] {
  const q = query.trim().toLowerCase();
  if (!q) return rows;
  const matches = (row: Row) =>
    row.text.toLowerCase().includes(q) || row.id.toLowerCase().includes(q);
  const matched = new Set(rows.filter(matches).map((r) => r.id));
  const shown = new Set<string>();
  for (const row of rows) {
    if (matched.has(row.id) || row.ancestors.some((a) => matched.has(a))) {
      shown.add(row.id);
      row.ancestors.forEach((a) => shown.add(a));
    }
  }
  return rows.filter((row) => shown.has(row.id));
}
type ComboboxProps = {
  value: string[];
  onChange: (value: string[]) => void;
  options: ComboboxNode[];
  placeholder?: ReactNode;
  disabled?: boolean;
  invalid?: boolean;
  className?: string;
  "aria-label"?: string;
};
export function Combobox({
  value,
  onChange,
  options,
  placeholder,
  disabled,
  invalid = false,
  className,
  "aria-label": ariaLabel,
}: ComboboxProps) {
  const field = useField();
  const id = useId();
  const reducedMotion = useReducedMotion();
  const [open, setOpen] = useState(false);
  usePopoverScrollLock(open);
  const settled = usePopoverSettled(open);
  const rows = useMemo(() => flatten(options), [options]);
  const byId = useMemo(() => new Map(rows.map((r) => [r.id, r])), [rows]);
  const order = useMemo(
    () => rows.filter((r) => !r.node.children).map((r) => r.id),
    [rows],
  );
  const picked = useMemo(() => new Set(value), [value]);
  const chips = useMemo(
    () =>
      compressPicked(options, picked)
        .map((chip) => byId.get(chip))
        .filter((row): row is Row => row !== undefined),
    [options, picked, byId],
  );
  // Choosing a row toggles its leaves: all picked → none, else all.
  const toggle = (row: Row) => {
    const next = new Set(picked);
    const all = row.leaves.every((leaf) => picked.has(leaf));
    for (const leaf of row.leaves) {
      if (all) next.delete(leaf);
      else next.add(leaf);
    }
    onChange(order.filter((leaf) => next.has(leaf)));
  };
  const state = useMemo(() => ({ picked, chips }), [picked, chips]);
  return (
    <LayoutGroup id={id}>
      <StateContext.Provider value={state}>
        <SettledContext.Provider value={settled}>
          <Select
            multi
            searchable
            options={rows}
            // BaseUI holds the chips; the rows read what's picked from
            // context, so a group shows checked, mixed or empty.
            value={chips}
            onChange={({ option, type }) => {
              if (type === "clear") onChange([]);
              else if (option)
                toggle(byId.get(String(option.id)) ?? (option as Row));
            }}
            onOpen={() => setOpen(true)}
            onClose={() => setOpen(false)}
            filterOptions={(list, query) => filterRows(list as Row[], query)}
            filterOutSelected={false}
            closeOnSelect={false}
            onSelectResetsInput={false}
            onBlurResetsInput
            backspaceRemoves={false}
            clearable={false}
            disabled={disabled ?? field?.disabled}
            error={invalid}
            placeholder={placeholder}
            aria-label={ariaLabel}
            aria-labelledby={ariaLabel ? undefined : field?.labelId}
            aria-describedby={field?.describedBy}
            labelKey="text"
            valueKey="id"
            getOptionLabel={({ option }) => (
              <ComboboxRowLabel row={option as Row} />
            )}
            getValueLabel={({ option }) => {
              const row = option as Row;
              return row.node.chip ?? row.text;
            }}
            overrides={{
              Root: { component: ListboxRoot, props: { className } },
              ControlContainer: { component: ListboxButton },
              ValueContainer: { component: ComboboxValueContainer },
              Placeholder: { component: ComboboxPlaceholder },
              InputContainer: { component: ComboboxInputContainer },
              Input: { component: ComboboxInput },
              MultiValue: { component: ComboboxChip },
              IconsContainer: { component: ListboxIcons },
              SelectArrow: { component: ChevronUpDown },
              Popover: {
                props: {
                  animateOutTime: reducedMotion ? 0 : popoverExit,
                  overrides: popoverOverrides,
                },
              },
              DropdownContainer: { component: ListboxPanel },
              Dropdown: { component: ListboxList },
              DropdownListItem: { component: ListboxOptionItem },
              OptionContent: { component: ComboboxOptionContent },
            }}
          />
        </SettledContext.Provider>
      </StateContext.Provider>
    </LayoutGroup>
  );
}
const ComboboxValueContainer = slot(
  "div",
  [
    "relative flex min-h-11 w-full flex-wrap items-center gap-1 rounded-lg py-[calc(--spacing(2)-1px)] sm:min-h-9 sm:py-[calc(--spacing(1)-1px)]",
    "pr-[calc(--spacing(7)-1px)] pl-[calc(--spacing(2.5)-1px)] sm:pl-[calc(--spacing(2)-1px)]",
    "text-left text-base/6 text-zinc-950 sm:text-sm/6 dark:text-white forced-colors:text-[CanvasText]",
    "border border-zinc-950/10 group-data-open:border-zinc-950/20 group-hover:border-zinc-950/20 dark:border-white/10 dark:group-data-open:border-white/20 dark:group-hover:border-white/20",
    "bg-transparent dark:bg-white/5",
    "group-data-invalid:border-red-500 dark:group-data-invalid:border-red-600",
    "group-data-disabled:border-zinc-950/20 dark:group-data-disabled:border-white/15",
  ].join(" "),
);
const ComboboxPlaceholder = slot(
  "span",
  "pointer-events-none absolute inset-y-0 right-7 left-[calc(--spacing(3.5)-1px)] flex items-center truncate text-zinc-500 sm:left-[calc(--spacing(3)-1px)]",
);
const ComboboxInputContainer = slot(
  "div",
  "relative flex min-w-16 flex-1 px-1",
);
const ComboboxInput = slot(
  "input",
  "w-full min-w-0 appearance-none bg-transparent py-0.5 text-base/6 text-zinc-950 outline-hidden sm:text-sm/6 dark:text-white",
  { ignore: ["$isFocused"] },
);
// A chip in the closed control: the first `maxChips`, then "+N" for the
// rest. Its × removes everything it stands for.
function ComboboxChip({
  value,
  onActionClick,
  children,
}: {
  value: Row;
  // BaseUI's remove, as its Tag's action.
  onActionClick: () => void;
  children?: ReactNode;
}) {
  const { chips } = useContext(StateContext);
  const index = chips.findIndex((chip) => chip.id === value.id);
  if (index >= maxChips) {
    if (index > maxChips) return null;
    return (
      <span className="px-1 text-sm/5 text-zinc-500 sm:text-xs/5 dark:text-zinc-400">
        +{chips.length - maxChips}
      </span>
    );
  }
  return (
    <Badge
      className="max-w-48 min-w-0"
      onRemove={onActionClick}
      removeLabel={`Remove ${String(children)}`}
    >
      <span className="truncate">{children}</span>
    </Badge>
  );
}
// A row: indented by depth, a check box (checked, empty, or a dash for a
// partly picked group), its label and, for a group, how many it holds.
function ComboboxRowLabel({ row }: { row: Row }) {
  const { picked } = useContext(StateContext);
  const count = row.leaves.filter((leaf) => picked.has(leaf)).length;
  const group = Boolean(row.node.children);
  const state =
    count === 0 ? "empty" : count === row.leaves.length ? "checked" : "mixed";
  const spoken = group
    ? `${row.text}, group, ${count} of ${row.leaves.length} selected`
    : `${row.text}, ${state === "checked" ? "selected" : "not selected"}`;
  return (
    <span
      className={cx(
        "col-span-2 flex min-w-0 items-center gap-x-2",
        indents[Math.min(row.depth, indents.length - 1)],
      )}
    >
      <span className="sr-only">{spoken}</span>
      <CheckBox state={state} />
      <span aria-hidden="true" className="min-w-0 flex-1 truncate">
        {row.node.label}
      </span>
      {group && (
        <span
          aria-hidden="true"
          className="shrink-0 text-xs/5 text-zinc-500 tabular-nums dark:text-zinc-400"
        >
          {row.leaves.length}
        </span>
      )}
    </span>
  );
}
const indents = ["", "pl-4", "pl-8", "pl-12"];
function CheckBox({ state }: { state: "checked" | "mixed" | "empty" }) {
  return (
    <span
      aria-hidden="true"
      data-state={state}
      className={cx(
        "grid size-4 shrink-0 place-items-center rounded-[0.25rem] border",
        state === "empty"
          ? "border-zinc-950/20 bg-white dark:border-white/20 dark:bg-white/5"
          : "border-blue-500 bg-blue-500 text-white forced-colors:bg-[Highlight]",
      )}
    >
      {state === "checked" && (
        <svg className="size-3 stroke-current" viewBox="0 0 16 16" fill="none">
          <path
            d="M4 8.5l3 3L12 4"
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      )}
      {state === "mixed" && (
        <svg className="size-3 stroke-current" viewBox="0 0 16 16" fill="none">
          <path d="M4 8h8" strokeWidth={2} strokeLinecap="round" />
        </svg>
      )}
    </span>
  );
}
// The highlighted (hovered or arrowed-to) row's grey background, sliding
// between rows as Listbox's does.
function ComboboxOptionContent({
  $isHighlighted,
  children,
}: {
  $isHighlighted?: boolean;
  children?: ReactNode;
}) {
  const settled = useContext(SettledContext);
  return (
    <>
      {$isHighlighted && (
        <Highlight
          slide={settled}
          className="-z-10 bg-zinc-950/5 dark:bg-white/10"
        />
      )}
      {children}
    </>
  );
}
