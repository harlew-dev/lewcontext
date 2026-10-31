import {
  Children,
  createContext,
  isValidElement,
  useContext,
  useEffect,
  useId,
  useState,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from "react";
import { Select, type Option } from "baseui/select";
import { LayoutGroup, useReducedMotion } from "motion/react";
import {
  popoverExit,
  popoverOverrides,
  popoverPanel,
  slot,
} from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { usePopoverScrollLock } from "@/lib/scroll-lock";
import { useField } from "./fieldset";
import { Highlight } from "./highlight";
// Listbox styles on BaseUI's Select, which provides the combobox
// keyboard handling, popover positioning and option highlighting. The open
// panel covers the button with the selected option over it (alignSelection).
const ListboxRoot = slot("div", "relative block w-full");
const ListboxButton = slot(
  "div",
  [
    "group relative block w-full",
    "before:absolute before:inset-px before:rounded-[calc(var(--radius-lg)-1px)] before:bg-white before:shadow-sm",
    "dark:before:hidden",
    "after:pointer-events-none after:absolute after:inset-0 after:rounded-lg after:ring-transparent after:ring-inset data-focus:after:ring-2 data-focus:after:ring-blue-500",
    "data-disabled:opacity-50 data-disabled:before:bg-zinc-950/5 data-disabled:before:shadow-none",
  ].join(" "),
  { props: () => ({ "data-slot": "control" }) },
);
const ListboxValueContainer = slot(
  "span",
  [
    "relative block w-full appearance-none rounded-lg py-[calc(--spacing(2.5)-1px)] sm:py-[calc(--spacing(1.5)-1px)]",
    "min-h-11 sm:min-h-9",
    "pr-[calc(--spacing(7)-1px)] pl-[calc(--spacing(3.5)-1px)] sm:pl-[calc(--spacing(3)-1px)]",
    "text-left text-base/6 text-zinc-950 placeholder:text-zinc-500 sm:text-sm/6 dark:text-white forced-colors:text-[CanvasText]",
    "border border-zinc-950/10 group-data-open:border-zinc-950/20 group-hover:border-zinc-950/20 dark:border-white/10 dark:group-data-open:border-white/20 dark:group-hover:border-white/20",
    "bg-transparent dark:bg-white/5",
    "group-data-invalid:border-red-500 group-hover:group-data-invalid:border-red-500 dark:group-data-invalid:border-red-600 dark:group-hover:group-data-invalid:border-red-600",
    "group-data-disabled:border-zinc-950/20 group-data-disabled:opacity-100 dark:group-data-disabled:border-white/15 dark:group-data-disabled:bg-white/2.5 dark:group-data-disabled:group-hover:border-white/15",
  ].join(" "),
);
const ListboxValue = slot(
  "div",
  "flex min-w-0 items-center *:data-[slot=icon]:size-5 *:data-[slot=icon]:shrink-0 sm:*:data-[slot=icon]:size-4 *:data-[slot=icon]:text-zinc-500 dark:*:data-[slot=icon]:text-zinc-400",
  { props: () => ({ "data-listbox-value": "" }) },
);
const ListboxPlaceholder = slot("span", "block truncate text-zinc-500");
// Non-searchable BaseUI selects focus this element; give it combobox semantics.
const ListboxFocusTarget = slot(
  "div",
  "absolute inset-0 rounded-lg focus:outline-hidden",
  {
    props: (state) => ({
      role: "combobox",
      "aria-haspopup": "listbox",
      "aria-expanded": Boolean(state.$isOpen),
    }),
  },
);
const ListboxIcons = slot(
  "span",
  "pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2",
);
function ChevronUpDown() {
  return (
    <svg
      className="size-5 stroke-zinc-500 group-data-disabled:stroke-zinc-600 sm:size-4 dark:stroke-zinc-400 forced-colors:stroke-[CanvasText]"
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
    >
      <path
        d="M5.75 10.75L8 13L10.25 10.75"
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M10.25 5.25L8 3L5.75 5.25"
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
// At least as wide as the button plus the check mark's gutter, which sits to
// the left of the button so the option text lines up with the button text.
const ListboxPanel = slot(
  "div",
  [
    "isolate w-max scroll-py-1 rounded-xl p-1 select-none",
    "min-w-[calc(var(--button-width)+1.75rem)] sm:min-w-[calc(var(--button-width)+1.5rem)] max-w-[calc(100vw-2rem)]",
    popoverPanel,
    "outline outline-transparent focus:outline-hidden",
    "overflow-y-auto overscroll-contain",
    "bg-white/75 backdrop-blur-xl dark:bg-zinc-800/75",
    "shadow-lg ring-1 ring-zinc-950/10 dark:ring-white/10 dark:ring-inset",
  ].join(" "),
  {
    props: (state) => ({
      "data-popover-panel": "",
      style: { "--button-width": `${state.$width as number}px` },
    }),
  },
);
const ListboxList = slot(
  "ul",
  "max-h-[min(20rem,calc(100dvh-2rem))] focus:outline-hidden",
);
// The highlighted (hovered or arrowed-to) option's background is a grey
// Highlight that slides between options, and the selected option is blue
// (ListboxOptionContent renders both). They sit at -z-10 and -z-5 in the
// panel's `isolate` stacking context, so the grey passes under every option's
// text, and under the blue, while it slides.
const ListboxOptionItem = slot(
  "li",
  [
    "group/option relative grid cursor-default grid-cols-[--spacing(5)_1fr] items-baseline gap-x-2 rounded-lg py-2.5 pr-3.5 pl-2 sm:grid-cols-[--spacing(4)_1fr] sm:py-1.5 sm:pr-3 sm:pl-1.5",
    "text-base/6 text-zinc-950 sm:text-sm/6 dark:text-white forced-colors:text-[CanvasText]",
    "outline-hidden",
    "forced-color-adjust-none forced-colors:data-focus:bg-[Highlight] forced-colors:data-focus:text-[HighlightText]",
    "data-disabled:opacity-50",
  ].join(" "),
  { ignore: ["$isFocused"] },
);
// Whether the open panel has settled where BaseUI positions it, so the
// highlight may slide (see `settleTime`).
const SettledContext = createContext(false);
// BaseUI positions the panel during its first frames (and it fades in for
// 100 ms); until then the highlight is drawn in place, without sliding.
const settleTime = 150;
function ListboxOptionContent({
  $selected,
  $isHighlighted,
  children,
}: {
  $selected?: boolean;
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
      {$selected && (
        <span
          aria-hidden="true"
          className="absolute inset-0 -z-5 rounded-lg bg-blue-500 forced-colors:hidden"
        />
      )}
      {$selected && (
        <svg
          className="relative size-5 self-center stroke-current text-white sm:size-4 forced-colors:text-[CanvasText]"
          viewBox="0 0 16 16"
          fill="none"
          aria-hidden="true"
        >
          <path
            d="M4 8.5l3 3L12 4"
            strokeWidth={1.5}
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      )}
      <span
        data-listbox-label
        className={cx(
          "col-start-2 flex min-w-0 items-center *:data-[slot=icon]:size-5 *:data-[slot=icon]:shrink-0 sm:*:data-[slot=icon]:size-4",
          $selected
            ? "text-white *:data-[slot=icon]:text-white forced-colors:text-[CanvasText]"
            : "*:data-[slot=icon]:text-zinc-500 dark:*:data-[slot=icon]:text-zinc-400",
        )}
      >
        {children}
      </span>
    </>
  );
}
// The part of popper.js's update data that alignSelection uses.
type Box = { top: number; left: number };
type PopperData = {
  instance: { reference: Element; popper: Element };
  offsets: { reference: Box; popper: Box };
};
// How close the open panel may come to the viewport's edges.
const viewportPadding = 16;
function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), Math.max(min, max));
}
// A popper.js modifier, run after BaseUI's placement on every update: moves
// the open panel so the selected option sits over the button, its text in
// line with the button's text (like Headless UI's "selection" anchor), while
// keeping the panel inside the viewport.
function alignSelection(data: PopperData): PopperData {
  const { reference, popper } = data.instance;
  const selected = popper.querySelector('[aria-selected="true"]');
  const label = selected?.querySelector("[data-listbox-label]");
  const value = reference.querySelector("[data-listbox-value]");
  if (!selected || !label || !value) return data;
  const button = reference.getBoundingClientRect();
  const panel = popper.getBoundingClientRect();
  const option = selected.getBoundingClientRect();
  // Where the selected option and its text sit inside the panel.
  const optionCentre = option.top - panel.top + option.height / 2;
  const labelLeft = label.getBoundingClientRect().left - panel.left;
  const top = button.top + button.height / 2 - optionCentre;
  const left = value.getBoundingClientRect().left - labelLeft;
  const viewport = document.documentElement;
  const maxTop = viewport.clientHeight - viewportPadding - panel.height;
  const maxLeft = viewport.clientWidth - viewportPadding - panel.width;
  // popper.js offsets are relative to the layer, not the viewport.
  data.offsets.popper.top =
    data.offsets.reference.top -
    button.top +
    clamp(top, viewportPadding, maxTop);
  data.offsets.popper.left =
    data.offsets.reference.left -
    button.left +
    clamp(left, viewportPadding, maxLeft);
  return data;
}
const popperOptions = {
  modifiers: {
    // The panel covers the button, so it never needs to flip above it.
    flip: { enabled: false },
    alignSelection: { enabled: true, order: 860, fn: alignSelection },
  },
};
type ListboxOptionProps<T extends string> = { value: T; children: ReactNode };
type ListboxProps<T extends string> = {
  value: T;
  onChange: (value: T) => void;
  children: ReactNode;
  placeholder?: ReactNode;
  disabled?: boolean;
  invalid?: boolean;
  className?: string;
  "aria-label"?: string;
};
export function Listbox<T extends string>({
  value,
  onChange,
  children,
  placeholder,
  disabled,
  invalid = false,
  className,
  "aria-label": ariaLabel,
}: ListboxProps<T>) {
  const field = useField();
  const id = useId();
  const reducedMotion = useReducedMotion();
  const [open, setOpen] = useState(false);
  const [settled, setSettled] = useState(false);
  // Nothing scrolls while open, as with a native select: the panel covers
  // the button, so scrolling would pull them apart.
  usePopoverScrollLock(open);
  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => setSettled(true), settleTime);
    return () => window.clearTimeout(timer);
  }, [open]);
  const options: Option[] = Children.toArray(children)
    .filter(isValidElement<ListboxOptionProps<T>>)
    .map((option) => ({
      id: option.props.value,
      label: option.props.children,
      text: textOf(option.props.children),
    }));
  const renderLabel = ({ option }: { option?: Option }) =>
    option?.label as ReactNode;
  return (
    // Its own LayoutGroup, so the highlight never slides in from another one.
    <LayoutGroup id={id}>
      <SettledContext.Provider value={settled}>
        <Select
          onOpen={() => setOpen(true)}
          onClose={() => {
            setOpen(false);
            setSettled(false);
          }}
          options={options}
          value={options.filter((option) => option.id === value)}
          onChange={({ value: selected }) => {
            if (selected[0]) onChange(selected[0].id as T);
          }}
          clearable={false}
          searchable={false}
          backspaceRemoves={false}
          disabled={disabled ?? field?.disabled}
          error={invalid}
          placeholder={placeholder}
          aria-label={ariaLabel}
          aria-labelledby={ariaLabel ? undefined : field?.labelId}
          aria-describedby={field?.describedBy}
          getOptionLabel={renderLabel}
          getValueLabel={renderLabel}
          // BaseUI names the control "Selected <labelKey>. <aria-label>", so it
          // needs the option's text, not its markup ("[object Object]").
          labelKey="text"
          overrides={{
            Root: { component: ListboxRoot, props: { className } },
            ControlContainer: { component: ListboxButton },
            ValueContainer: { component: ListboxValueContainer },
            SingleValue: { component: ListboxValue },
            Placeholder: { component: ListboxPlaceholder },
            InputContainer: { component: ListboxFocusTarget },
            IconsContainer: { component: ListboxIcons },
            SelectArrow: { component: ChevronUpDown },
            Popover: {
              props: {
                animateOutTime: reducedMotion ? 0 : popoverExit,
                popperOptions,
                overrides: popoverOverrides,
              },
            },
            DropdownContainer: { component: ListboxPanel },
            Dropdown: { component: ListboxList },
            DropdownListItem: { component: ListboxOptionItem },
            OptionContent: { component: ListboxOptionContent },
          }}
        />
      </SettledContext.Provider>
    </LayoutGroup>
  );
}
// The text inside an option's markup, e.g. "Planned" from
// <ListboxLabel>Planned</ListboxLabel>.
function textOf(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (isValidElement<{ children?: ReactNode }>(node))
    return textOf(node.props.children);
  return "";
}
// Declares an option; Listbox reads its props, so it renders nothing itself.
export function ListboxOption<T extends string>(
  props: ListboxOptionProps<T>,
): null {
  void props;
  return null;
}
export function ListboxLabel({
  className,
  ...props
}: ComponentPropsWithoutRef<"span">) {
  return (
    <span
      {...props}
      className={cx(
        className,
        "ml-2.5 truncate first:ml-0 sm:ml-2 sm:first:ml-0",
      )}
    />
  );
}
