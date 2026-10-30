import { useEffect, useState } from "react";
import { StatusPage } from "@/components/layout/status-page";
import { Button } from "@/components/ui/button";
import { useApi } from "@/lib/api";
import { routes } from "@/lib/routes";
// The public homepage, prerendered at build time (react-router.config.ts) so
// its title, description and content are in the HTML. It is built without a
// session, so it starts with "Log in" and offers the dashboard once the
// browser finds the visitor signed in.
export default function Home() {
  const signedIn = useSignedIn();
  return (
    <StatusPage
      title="Your work, in good shape."
      description="A little clarity for your next big thing. Your projects, together in one thoughtful workspace."
      action={
        signedIn ? (
          <Button href={routes.app}>Dashboard</Button>
        ) : (
          <Button href={routes.login}>Log in</Button>
        )
      }
    />
  );
}
// Whether the visitor has a session. Signed-out visitors, and an API that
// can't be reached, leave it false.
function useSignedIn(): boolean {
  const api = useApi();
  const [signedIn, setSignedIn] = useState(false);
  useEffect(() => {
    let current = true;
    api
      .auth()
      .me()
      .then(
        () => current && setSignedIn(true),
        () => undefined,
      );
    return () => {
      current = false;
    };
  }, [api]);
  return signedIn;
}
