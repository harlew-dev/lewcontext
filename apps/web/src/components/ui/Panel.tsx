import type { ReactNode } from "react";
// Bordered dashboard card.
export function Panel({ children }: { children: ReactNode }) {
  return (
    <section className="rounded-[9px] border border-line p-5.25 max-md:p-4.5">
      {children}
    </section>
  );
}
