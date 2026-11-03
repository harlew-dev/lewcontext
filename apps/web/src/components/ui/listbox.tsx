import {
  Children,
  isValidElement,
  useId,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from "react";
import { Select } from "@base-ui/react/select";
import { LayoutGroup } from "motion/react";
import { cx } from "@/lib/cx";
import { popoverPanel } from "@/lib/transitions";
import { useField } from "./fieldset";
import { Highlight } from "./highlight";
// Listbox on Base UI's Select, which provides the combobox keyboard
// handling, positioning and option highlighting. The open panel covers the
// button with the selected option over it (the positioner's
// alignItemWithTrigger, off for touch). Combobox (combobox.tsx) shares its
// button, panel and option classes.
export const listboxButtonClasses = [
  "group relative block w-full",
  "before:absolute before:inset-px before:rounded-[calc(var(--radius-lg)-1px)] before:bg-white before:shadow-sm",
  "dark:before:hidden",
  "after:pointer-events-none after:absolute after:inset-0 after:rounded-lg after:ring-transparent after:ring-inset focus-visible:after:ring-2 focus-visible:after:ring-blue-500 focus-within:after:ring-2 focus-within:after:ring-blue-500",
  "data-disabled:opacity-50 data-disabled:before:bg-zinc-950/5 data-disabled:before:shadow-none",
  "focus:outline-hidden",
].join(" ");
export const listboxValueClasses = [
  "relative block w-full appearance-none rounded-lg py-[calc(--spacing(2.5)-1px)] sm:py-[calc(--spacing(1.5)-1px)]",
  "min-h-11 sm:min-h-9",
  "pr-[calc(--spacing(7)-1px)] pl-[calc(--spacing(3.5)-1px)] sm:pl-[calc(--spacing(3)-1px)]",
  "text-left text-base/6 text-zinc-950 placeholder:text-zinc-500 sm:text-sm/6 dark:text-white forced-colors:text-[CanvasText]",
  "border border-zinc-950/10 group-data-popup-open:border-zinc-950/20 group-hover:border-zinc-950/20 dark:border-white/10 dark:group-data-popup-open:border-white/20 dark:group-hover:border-white/20",
  "bg-transparent dark:bg-white/5",
  "group-data-invalid:border-red-500 group-hover:group-data-invalid:border-red-500 dark:group-data-invalid:border-red-600 dark:group-hover:group-data-invalid:border-red-600",
  "group-data-disabled:border-zinc-950/20 group-data-disabled:opacity-100 dark:group-data-disabled:border-white/15 dark:group-data-disabled:bg-white/2.5 dark:group-data-disabled:group-hover:border-white/15",
].join(" ");
const valueClasses =
  "flex min-w-0 items-center *:data-[slot=icon]:size-5 *:data-[slot=icon]:shrink-0 sm:*:data-[slot=icon]:size-4 *:data-[slot=icon]:text-zinc-500 dark:*:data-[slot=icon]:text-zinc-400";
export const listboxIconsClasses =
  "pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2";
export function ChevronUpDown() {
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
export const listboxPanelClasses = [
  "isolate w-max origin-(--transform-origin) rounded-xl p-1 select-none",
  "min-w-[calc(var(--anchor-width)+1.75rem)] sm:min-w-[calc(var(--anchor-width)+1.5rem)] max-w-[calc(100vw-2rem)]",
  popoverPanel,
  "outline outline-transparent focus:outline-hidden",
  "bg-white/75 backdrop-blur-xl dark:bg-zinc-800/75",
  "shadow-lg ring-1 ring-zinc-950/10 dark:ring-white/10 dark:ring-inset",
].join(" ");
export const listboxListClasses =
  "max-h-[min(20rem,calc(100dvh-2rem))] scroll-py-1 overflow-y-auto overscroll-contain focus:outline-hidden";
// The highlighted (hovered or arrowed-to) option's background is a grey
// Highlight that slides between options, and the selected option is blue
// (renderOption draws both). They sit at -z-10 and -z-5 in the panel's
// `isolate` stacking context, so the grey passes under every option's text,
// and under the blue, while it slides.
export const listboxOptionClasses = [
  "group/option relative grid cursor-default grid-cols-[--spacing(5)_1fr] items-baseline gap-x-2 rounded-lg py-2.5 pr-3.5 pl-2 sm:grid-cols-[--spacing(4)_1fr] sm:py-1.5 sm:pr-3 sm:pl-1.5",
  "text-base/6 text-zinc-950 sm:text-sm/6 dark:text-white forced-colors:text-[CanvasText]",
  "outline-hidden",
  "forced-color-adjust-none forced-colors:data-highlighted:bg-[Highlight] forced-colors:data-highlighted:text-[HighlightText]",
  "data-disabled:opacity-50",
].join(" ");
function ListboxOptionContent({
  selected,
  highlighted,
  children,
}: {
  selected: boolean;
  highlighted: boolean;
  children?: ReactNode;
}) {
  return (
    <>
      {highlighted && (
        <Highlight className="-z-10 bg-zinc-950/5 dark:bg-white/10" />
      )}
      {selected && (
        <span
          aria-hidden="true"
          className="absolute inset-0 -z-5 rounded-lg bg-blue-500 forced-colors:hidden"
        />
      )}
      {selected && (
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
      <Select.ItemText
        data-listbox-label
        className={cx(
          "col-start-2 flex min-w-0 items-center *:data-[slot=icon]:size-5 *:data-[slot=icon]:shrink-0 sm:*:data-[slot=icon]:size-4",
          selected
            ? "text-white *:data-[slot=icon]:text-white forced-colors:text-[CanvasText]"
            : "*:data-[slot=icon]:text-zinc-500 dark:*:data-[slot=icon]:text-zinc-400",
        )}
      >
        {children}
      </Select.ItemText>
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
  const id = useId();
  const options = Children.toArray(children).filter(
    isValidElement<ListboxOptionProps<T>>,
  );
  const labelOf = (selected: string | null) =>
    options.find((option) => option.props.value === selected)?.props.children;
  const isDisabled = disabled ?? field?.disabled;
  return (
    // Its own LayoutGroup, so the highlight never slides in from another one.
    <LayoutGroup id={id}>
      <Select.Root
        value={options.some((o) => o.props.value === value) ? value : null}
        onValueChange={(selected) => {
          if (selected !== null) onChange(selected as T);
        }}
        disabled={isDisabled}
      >
        <div className={cx("relative block w-full", className)}>
          <Select.Trigger
            data-slot="control"
            data-invalid={invalid ? "" : undefined}
            aria-label={ariaLabel}
            aria-labelledby={ariaLabel ? undefined : field?.labelId}
            aria-describedby={field?.describedBy}
            aria-invalid={invalid || undefined}
            className={listboxButtonClasses}
          >
            <span className={listboxValueClasses}>
              <Select.Value className={valueClasses}>
                {(selected: string | null) =>
                  selected === null ? (
                    <span className="block truncate text-zinc-500">
                      {placeholder}
                    </span>
                  ) : (
                    labelOf(selected)
                  )
                }
              </Select.Value>
            </span>
            <Select.Icon className={listboxIconsClasses}>
              <ChevronUpDown />
            </Select.Icon>
          </Select.Trigger>
        </div>
        <Select.Portal>
          <Select.Positioner className="z-[60] outline-hidden">
            <Select.Popup className={listboxPanelClasses}>
              <Select.List className={listboxListClasses}>
                {options.map((option) => (
                  <Select.Item
                    key={option.props.value}
                    value={option.props.value}
                    className={listboxOptionClasses}
                    render={(props, state) => (
                      <div {...props}>
                        <ListboxOptionContent
                          selected={state.selected}
                          highlighted={state.highlighted}
                        >
                          {option.props.children}
                        </ListboxOptionContent>
                      </div>
                    )}
                  />
                ))}
              </Select.List>
            </Select.Popup>
          </Select.Positioner>
        </Select.Portal>
      </Select.Root>
    </LayoutGroup>
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
