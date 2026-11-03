// Joins conditional Tailwind class names, skipping falsy entries.
export function cx(...classes: Array<string | false | null | undefined>) {
  return classes.filter(Boolean).join(" ");
}
