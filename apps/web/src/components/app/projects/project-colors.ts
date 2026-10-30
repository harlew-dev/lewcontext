import type { Project } from "@/lib/types";
// Project colour classes, shared by the table's avatars and the dialog's swatches.
// Full class strings so Tailwind can detect them.
export const projectColors: Record<Project["color"], string> = {
  violet: "bg-violet-500 text-white",
  blue: "bg-sky-500 text-white",
  amber: "bg-amber-400 text-amber-950",
  emerald: "bg-emerald-500 text-white",
  rose: "bg-rose-500 text-white",
};
