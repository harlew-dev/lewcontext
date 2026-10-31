import type { ReactNode } from "react";
import { Divider } from "./divider";
// A headline number under a rule: its title, the value ("–" until it has
// loaded), and a line of context below.
export function Stat({
  title,
  value,
  children,
}: {
  title: string;
  value: ReactNode | undefined;
  children?: ReactNode;
}) {
  return (
    <div>
      <Divider />
      <div className="mt-6 text-lg/6 font-medium sm:text-sm/6">{title}</div>
      <div className="mt-3 text-3xl/8 font-semibold sm:text-2xl/8">
        {value ?? "–"}
      </div>
      {children && (
        <div className="mt-3 text-sm/6 sm:text-xs/6">{children}</div>
      )}
    </div>
  );
}
