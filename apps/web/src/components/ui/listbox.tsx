import {
  Children,
  isValidElement,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from "react";
import { Select, type Option } from "baseui/select";
import { slot, transparentSurface } from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { useField } from "./fieldset";
// Listbox styles on BaseUI's Select, which provides the combobox
// keyboard handling, popover positioning and option highlighting.
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
const ListboxPanel = slot(
  "div",
  [
    "mt-1 isolate min-w-max scroll-py-1 rounded-xl p-1 select-none",
    "outline outline-transparent focus:outline-hidden",
    "overflow-y-auto overscroll-contain",
    "bg-white/75 backdrop-blur-xl dark:bg-zinc-800/75",
    "shadow-lg ring-1 ring-zinc-950/10 dark:ring-white/10 dark:ring-inset",
  ].join(" "),
  { props: (state) => ({ style: { width: state.$width as number } }) },
);
const ListboxList = slot("ul", "max-h-80 focus:outline-hidden");
const ListboxOptionItem = slot(
  "li",
  [
    "group/option grid cursor-default grid-cols-[--spacing(5)_1fr] items-baseline gap-x-2 rounded-lg py-2.5 pr-3.5 pl-2 sm:grid-cols-[--spacing(4)_1fr] sm:py-1.5 sm:pr-3 sm:pl-1.5",
    "text-base/6 text-zinc-950 sm:text-sm/6 dark:text-white forced-colors:text-[CanvasText]",
    "outline-hidden data-focus:bg-blue-500 data-focus:text-white",
    "forced-color-adjust-none forced-colors:data-focus:bg-[Highlight] forced-colors:data-focus:text-[HighlightText]",
    "data-disabled:opacity-50",
  ].join(" "),
  { ignore: ["$isFocused"] },
);
function ListboxOptionContent({
  $selected,
  children,
}: {
  $selected?: boolean;
  children?: ReactNode;
}) {
  return (
    <>
      {$selected && (
        <svg
          className="relative size-5 self-center stroke-current sm:size-4"
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
      <span className="col-start-2 flex min-w-0 items-center *:data-[slot=icon]:size-5 *:data-[slot=icon]:shrink-0 sm:*:data-[slot=icon]:size-4 *:data-[slot=icon]:text-zinc-500 group-data-focus/option:*:data-[slot=icon]:text-white dark:*:data-[slot=icon]:text-zinc-400">
        {children}
      </span>
    </>
  );
}
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
  const options: Option[] = Children.toArray(children)
    .filter(isValidElement<ListboxOptionProps<T>>)
    .map((option) => ({
      id: option.props.value,
      label: option.props.children,
    }));
  const renderLabel = ({ option }: { option?: Option }) =>
    option?.label as ReactNode;
  return (
    <Select
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
            overrides: { Body: transparentSurface, Inner: transparentSurface },
          },
        },
        DropdownContainer: { component: ListboxPanel },
        Dropdown: { component: ListboxList },
        DropdownListItem: { component: ListboxOptionItem },
        OptionContent: { component: ListboxOptionContent },
      }}
    />
  );
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
