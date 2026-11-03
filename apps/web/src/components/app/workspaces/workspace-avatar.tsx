import { Avatar } from "@/components/ui/avatar";
import { cx } from "@/lib/cx";
import type { Workspace } from "@/types/workspaces";
// A workspace's first letter on a square tile, in the switcher and settings.
export function WorkspaceAvatar({
  workspace,
  className,
}: {
  workspace: Pick<Workspace, "name">;
  className?: string;
}) {
  return (
    <Avatar
      square
      initials={Array.from(workspace.name.trim())[0] ?? "?"}
      className={cx(
        className,
        "bg-zinc-900 text-white dark:bg-white dark:text-zinc-950",
      )}
    />
  );
}
