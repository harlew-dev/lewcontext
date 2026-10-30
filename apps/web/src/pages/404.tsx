import { StatusPage } from "@/components/layout/status-page";
import { Button } from "@/components/ui/button";
import { routes } from "@/lib/routes";
export default function NotFound() {
  return (
    <StatusPage
      title="This page has wandered off."
      description="Let’s get you back to your workspace."
      action={<Button href={routes.dashboard}>Back to overview</Button>}
    />
  );
}
