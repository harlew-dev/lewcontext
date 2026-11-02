import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Reference } from "@/components/ui/feedback";
import { Heading, HeadingGroup } from "@/components/ui/heading";
import { Logo } from "@/components/ui/logo";
import { Text } from "@/components/ui/text";
import { routes } from "@/lib/routes";
import { AuthLayout } from "./auth-layout";
// Full-page message (homepage, 404, service outage) in the auth layout,
// with a failure's reference under the description when there is one.
export function StatusPage({
  title,
  description,
  action,
  footer,
  requestId,
}: {
  title: string;
  description: string;
  action: ReactNode;
  // Below the card (the homepage's policy links).
  footer?: ReactNode;
  requestId?: string;
}) {
  return (
    <AuthLayout footer={footer}>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <Logo />
        <HeadingGroup>
          <Heading>{title}</Heading>
          <Text>{description}</Text>
          {requestId && (
            <Text>
              <Reference requestId={requestId} />
            </Text>
          )}
        </HeadingGroup>
        <div>{action}</div>
      </div>
    </AuthLayout>
  );
}
// Unknown URLs, and records that don't exist or aren't the member's.
export function NotFound() {
  return (
    <StatusPage
      title="This page has wandered off."
      description="Let’s get you back to your workspace."
      action={<Button href={routes.app}>Back to your workspace</Button>}
    />
  );
}
// The outage page. requestId is the failed request's ID, when the API sent
// one (a 500): its reference, for support to find the log line.
export function ServiceUnavailable({ requestId }: { requestId?: string }) {
  return (
    <StatusPage
      title="A brief pause."
      description="We’re having trouble reaching your workspace. Your data is safe — please try again in a moment."
      requestId={requestId}
      action={
        <Button onClick={() => window.location.reload()}>Try again</Button>
      }
    />
  );
}
