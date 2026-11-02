import {
  createContext,
  useContext,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Combobox as BaseCombobox } from "@base-ui/react/combobox";
import { LayoutGroup } from "motion/react";
import { cx } from "@/lib/cx";
import { Badge } from "./badge";
import { useField } from "./fieldset";
import { Highlight } from "./highlight";
import {
  ChevronUpDown,
  listboxButtonClasses,
  listboxIconsClasses,
  listboxListClasses,
  listboxOptionClasses,
  listboxPanelClasses,
} from "./listbox";
// A searchable multi-select over a tree: groups of options, picked whole or
// one by one. On Base UI's multiple Combobox (kept open while picking),
// styled like Listbox. Its value is the leaves picked; choosing a
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
type Row = {
  id: string;
  text: string;
  node: ComboboxNode;
  depth: number;
  leaves: string[];
  ancestors: string[];
};
// How many chips the closed control shows before "+N".
const maxChips = 2;
// What rows and chips read while Base UI renders them: the picked leaves and
// the chips.
type ComboboxState = { picked: Set<string>; chips: Row[] };
const StateContext = createContext<ComboboxState>({
  picked: new Set(),
  chips: [],
});
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
  placeholder?: string;
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
  const anchor = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
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
  const shown = useMemo(() => filterRows(rows, query), [rows, query]);
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
  // Base UI holds the chips and reports each choice as one added or removed
  // row; what that row stands for is ours to toggle.
  const choose = (next: Row[]) => {
    const before = new Set(chips.map((row) => row.id));
    const after = new Set(next.map((row) => row.id));
    const changed =
      next.find((row) => !before.has(row.id)) ??
      chips.find((row) => !after.has(row.id));
    if (changed) toggle(changed);
  };
  const state = useMemo(() => ({ picked, chips }), [picked, chips]);
  const isDisabled = disabled ?? field?.disabled;
  return (
    <LayoutGroup id={id}>
      <StateContext.Provider value={state}>
        <BaseCombobox.Root<Row, true>
          multiple
          items={rows}
          filteredItems={shown}
          filter={null}
          value={chips}
          onValueChange={choose}
          open={open}
          onOpenChange={(next, details) => {
            // Picking a row keeps the list open, for the next pick.
            if (!next && details.reason === "item-press")
              return details.cancel();
            setOpen(next);
            if (!next) setQuery("");
          }}
          inputValue={query}
          onInputValueChange={(next, details) => {
            if (details.reason !== "item-press") setQuery(next);
          }}
          isItemEqualToValue={(a, b) => a.id === b.id}
          itemToStringLabel={(row) => row.text}
          itemToStringValue={(row) => row.id}
          disabled={isDisabled}
        >
          <div className={cx("relative block w-full", className)}>
            <div
              ref={anchor}
              data-slot="control"
              data-invalid={invalid ? "" : undefined}
              data-disabled={isDisabled ? "" : undefined}
              onClick={() => {
                input.current?.focus();
                setOpen(true);
              }}
              className={listboxButtonClasses}
            >
              <div className={comboboxValueClasses}>
                {chips.slice(0, maxChips).map((row) => (
                  <Badge
                    key={row.id}
                    className="max-w-48 min-w-0"
                    onRemove={() => toggle(row)}
                    removeLabel={`Remove ${row.node.chip ?? row.text}`}
                  >
                    <span className="truncate">
                      {row.node.chip ?? row.text}
                    </span>
                  </Badge>
                ))}
                {chips.length > maxChips && (
                  <span className="px-1 text-sm/5 text-zinc-500 sm:text-xs/5 dark:text-zinc-400">
                    +{chips.length - maxChips}
                  </span>
                )}
                <div className="relative flex min-w-16 flex-1 px-1">
                  <BaseCombobox.Input
                    ref={input}
                    id={field?.id}
                    placeholder={chips.length === 0 ? placeholder : undefined}
                    aria-label={ariaLabel}
                    aria-labelledby={ariaLabel ? undefined : field?.labelId}
                    aria-describedby={field?.describedBy}
                    aria-invalid={invalid || undefined}
                    // Backspace in an empty search removes nothing.
                    onKeyDown={(event) => {
                      if (event.key === "Backspace" && query === "")
                        event.preventBaseUIHandler();
                    }}
                    className="w-full min-w-0 appearance-none bg-transparent py-0.5 text-base/6 text-zinc-950 outline-hidden placeholder:text-zinc-500 sm:text-sm/6 dark:text-white"
                  />
                </div>
              </div>
              <span className={listboxIconsClasses}>
                <ChevronUpDown />
              </span>
            </div>
          </div>
          <BaseCombobox.Portal>
            <BaseCombobox.Positioner
              anchor={anchor}
              sideOffset={4}
              className="z-[60] outline-hidden"
            >
              <BaseCombobox.Popup className={listboxPanelClasses}>
                <BaseCombobox.List className={listboxListClasses}>
                  {(row: Row) => <ComboboxOption key={row.id} row={row} />}
                </BaseCombobox.List>
              </BaseCombobox.Popup>
            </BaseCombobox.Positioner>
          </BaseCombobox.Portal>
        </BaseCombobox.Root>
      </StateContext.Provider>
    </LayoutGroup>
  );
}
const comboboxValueClasses = [
  "relative flex min-h-11 w-full flex-wrap items-center gap-1 rounded-lg py-[calc(--spacing(2)-1px)] sm:min-h-9 sm:py-[calc(--spacing(1)-1px)]",
  "pr-[calc(--spacing(7)-1px)] pl-[calc(--spacing(2.5)-1px)] sm:pl-[calc(--spacing(2)-1px)]",
  "text-left text-base/6 text-zinc-950 sm:text-sm/6 dark:text-white forced-colors:text-[CanvasText]",
  "border border-zinc-950/10 group-data-popup-open:border-zinc-950/20 group-hover:border-zinc-950/20 dark:border-white/10 dark:group-hover:border-white/20",
  "bg-transparent dark:bg-white/5",
  "group-data-invalid:border-red-500 dark:group-data-invalid:border-red-600",
  "group-data-disabled:border-zinc-950/20 dark:group-data-disabled:border-white/15",
].join(" ");
// Whether a row is picked: all its leaves, none, or some (a group).
function pickedState(row: Row, picked: Set<string>) {
  const count = row.leaves.filter((leaf) => picked.has(leaf)).length;
  const state: "checked" | "mixed" | "empty" =
    count === 0 ? "empty" : count === row.leaves.length ? "checked" : "mixed";
  return { count, state };
}
const ariaChecked = {
  checked: "true",
  mixed: "mixed",
  empty: "false",
} as const;
// A row's option: the picked state is aria-checked ("true", "false" or
// "mixed"; Base UI's aria-selected only knows the chips), with the grey
// Highlight that slides between rows as Listbox's does.
function ComboboxOption({ row }: { row: Row }) {
  const { picked } = useContext(StateContext);
  const checked = ariaChecked[pickedState(row, picked).state];
  return (
    <BaseCombobox.Item
      value={row}
      aria-checked={checked}
      aria-selected={checked === "true"}
      className={listboxOptionClasses}
      render={(props, state) => (
        <div {...props}>
          {state.highlighted && (
            <Highlight className="-z-10 bg-zinc-950/5 dark:bg-white/10" />
          )}
          <ComboboxRowLabel row={row} />
        </div>
      )}
    />
  );
}
// A row: indented by depth, a check box (checked, empty, or a dash for a
// partly picked group), its label and, for a group, how many it holds.
function ComboboxRowLabel({ row }: { row: Row }) {
  const { picked } = useContext(StateContext);
  const { count, state } = pickedState(row, picked);
  const group = Boolean(row.node.children);
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
