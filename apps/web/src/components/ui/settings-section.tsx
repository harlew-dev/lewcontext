import type { ReactNode } from "react";
import { Subheading } from "./heading";
import { Text } from "./text";
// A settings group: title and description on the left, content on the right
// (stacked on small screens).
export function SettingsSection({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section className="grid gap-x-8 gap-y-6 sm:grid-cols-2">
      <div className="space-y-1">
        <Subheading>{title}</Subheading>
        <Text>{description}</Text>
      </div>
      {children}
    </section>
  );
}
