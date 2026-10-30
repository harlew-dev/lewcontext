import { StatusPage } from "@/components/ui/Feedback";
import { TextLink } from "@/components/ui/TextLink";
import { routes } from "@/lib/routes";
export default function NotFound() {
  return (
    <StatusPage eyebrow="A SMALL DETOUR" title="This page has wandered off.">
      <p>Let’s get you back to your workspace.</p>
      <TextLink href={routes.dashboard}>Back to overview →</TextLink>
    </StatusPage>
  );
}
