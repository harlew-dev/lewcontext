import { AuthLayout } from "@/components/layout/auth-layout";
import { Heading, HeadingGroup } from "@/components/ui/heading";
import { Logo } from "@/components/ui/logo";
import { Text } from "@/components/ui/text";
import { appName } from "@/lib/app";
import { routes } from "@/lib/routes";
import type { Route } from "./+types/index";
// /docs, prerendered: the public API documentation, linked from the API key
// secret step. It holds no user data and no session.
export const meta: Route.MetaFunction = () => [
  { title: `API Documentation · ${appName}` },
  {
    name: "description",
    content: `API documentation for ${appName}.`,
  },
];
export default function Documentation() {
  return (
    <AuthLayout>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <Logo href={routes.home} />
        <HeadingGroup>
          <Heading>API Documentation</Heading>
          <Text>Not documented yet.</Text>
        </HeadingGroup>
      </div>
    </AuthLayout>
  );
}
