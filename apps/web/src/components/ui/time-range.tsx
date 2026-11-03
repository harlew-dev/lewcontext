import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
// The time range of a log: a Listbox of presets and "Custom range…", which
// the page opens in a RangeDialog. A custom range shows as its own value
// (and is chosen again to change it). The admin dashboard's Events and a
// workspace's Logs share it.
export function TimeRange<T extends string>({
  value,
  presets,
  custom,
  onPreset,
  onCustom,
  className = "sm:w-60",
}: {
  // The preset in use, ignored while `custom` is set.
  value: T;
  presets: readonly { id: T; label: string }[];
  // The custom range's label when one is applied, else null.
  custom: string | null;
  onPreset: (preset: T) => void;
  onCustom: () => void;
  className?: string;
}) {
  return (
    <Listbox<string>
      aria-label="Time range"
      value={custom ? "custom" : value}
      onChange={(v) => (v === "custom" ? onCustom() : onPreset(v as T))}
      className={className}
    >
      {presets.map((preset) => (
        <ListboxOption key={preset.id} value={preset.id}>
          <ListboxLabel>{preset.label}</ListboxLabel>
        </ListboxOption>
      ))}
      <ListboxOption value="custom">
        <ListboxLabel>{custom ?? "Custom range…"}</ListboxLabel>
      </ListboxOption>
    </Listbox>
  );
}
