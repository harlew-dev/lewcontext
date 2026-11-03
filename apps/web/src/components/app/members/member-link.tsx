import { TextLink } from "@/components/ui/text";
import { routes } from "@/lib/routes";
// A member's name, linking to their page where the viewer may open it
// (workspace.member.show) and the name is known; plain text otherwise (a
// deleted account, or a screen with no workspace to open it in).
export function MemberLink({
  workspace,
  member,
  fallback = "A deleted account",
  className,
}: {
  // The workspace to open the page in, or null to show text.
  workspace: string | null;
  member: { id: string; name?: string } | null;
  fallback?: string;
  className?: string;
}) {
  if (!member?.name) return <span className={className}>{fallback}</span>;
  if (!workspace) return <span className={className}>{member.name}</span>;
  return (
    <TextLink
      href={routes.members.show(workspace, member.id)}
      className={className}
    >
      {member.name}
    </TextLink>
  );
}
