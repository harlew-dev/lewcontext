import { useState } from "react";
import { Outlet, useLocation } from "react-router";
import {
  PageTransition,
  type TransitionHandle,
  type TransitionSide,
} from "@/components/layout/page-transition";
import {
  SettingsTabs,
  settingsTabs,
} from "@/components/app/workspaces/settings-tabs";
import { Divider } from "@/components/ui/divider";
import { Heading } from "@/components/ui/heading";
import { useWorkspace } from "@/lib/workspace";
// Workspace settings' heading and tabs, around General and Members. They stay
// mounted between the tabs, so the current tab's underline slides; the app
// layout doesn't fade the page in again (a tab group), and the content below
// slides in from the side of the tab chosen instead.
export const handle: TransitionHandle = { tabGroup: true };
export default function WorkspaceSettingsLayout() {
  const workspace = useWorkspace();
  const { pathname } = useLocation();
  const index = settingsTabs(workspace.id).findIndex(
    (tab) => tab.href === pathname,
  );
  const from = useSlideFrom(pathname, index);
  return (
    <div className="mx-auto max-w-4xl">
      <Heading>Workspace settings</Heading>
      <SettingsTabs />
      <Divider className="my-10 mt-6" />
      {from ? (
        <PageTransition key={pathname} from={from}>
          <Outlet />
        </PageTransition>
      ) : (
        <Outlet />
      )}
    </div>
  );
}
// The side the content comes in from: towards the tab chosen. Nothing on
// arrival (the app layout fades the page in) or when only the workspace
// changed. Worked out while rendering, so the first frame already has it.
function useSlideFrom(
  pathname: string,
  index: number,
): TransitionSide | undefined {
  const [shown, setShown] = useState<{
    pathname: string;
    index: number;
    from?: TransitionSide;
  }>({ pathname, index });
  if (shown.pathname === pathname) return shown.from;
  const from: TransitionSide | undefined =
    index === shown.index ? undefined : index > shown.index ? "right" : "left";
  setShown({ pathname, index, from });
  return from;
}
