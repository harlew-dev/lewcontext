import { Outlet, useLocation } from "react-router";
import { Heading } from "@/components/ui/heading";
import { Divider } from "@/components/ui/divider";
import { Navbar, NavbarSection, NavbarItem } from "@/components/ui/navbar";
import {
  PageTransition,
  type TransitionHandle,
} from "@/components/layout/page-transition";
import { routes } from "@/lib/routes";

export const handle: TransitionHandle = { tabGroup: true };
export default function AccountSettingsLayout() {
  const { pathname } = useLocation();
  const keys = pathname === routes.userApiKeys.index();
  return (
    <div className="mx-auto max-w-4xl">
      <Heading>Account &amp; security</Heading>
      <Navbar aria-label="Account settings" className="mt-4">
        <NavbarSection className="-ml-2">
          <NavbarItem href={routes.settings} current={!keys}>
            Settings
          </NavbarItem>
          <NavbarItem href={routes.userApiKeys.index()} current={keys}>
            API Keys
          </NavbarItem>
        </NavbarSection>
      </Navbar>
      <Divider className="my-10 mt-6" />
      <PageTransition key={pathname} from={keys ? "right" : "left"}>
        <Outlet />
      </PageTransition>
    </div>
  );
}
