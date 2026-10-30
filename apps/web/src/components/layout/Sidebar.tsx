import Link from "next/link";
import { useRouter } from "next/router";
import { ChevronsUpDown, ArrowUpRight, LogOut, CircleHelp } from "lucide-react";
import { Brand } from "@/components/ui/Brand";
import { IconButton } from "@/components/ui/IconButton";
import { cx } from "@/lib/cx";
import { navigation, routes } from "@/lib/routes";
import type { User } from "@/lib/types";
export function Sidebar({
  user,
  onLogout,
  loggingOut,
  onHelp,
}: {
  user: User;
  onLogout: () => void;
  loggingOut: boolean;
  onHelp: () => void;
}) {
  return (
    <aside className="fixed top-0 bottom-0 left-0 flex w-[242px] flex-col px-4.75 pt-7.5 pb-4 max-lg:w-[210px] max-lg:px-3 max-md:static max-md:grid max-md:w-auto max-md:grid-cols-[1fr_auto] max-md:gap-4.5 max-md:p-4.5 max-sm:flex max-sm:flex-row max-sm:items-center max-sm:justify-between max-sm:gap-1.25 max-sm:px-1.25">
      <Link
        href={routes.dashboard}
        className="mb-8.5 block pl-3.5 max-md:m-0 max-md:p-0"
        aria-label="Forma home"
      >
        <Brand />
      </Link>
      <WorkspaceSwitcher />
      <p className="mx-3.5 mb-2.75 text-tiny font-semibold tracking-[1.3px] text-haze-400 max-md:hidden">
        WORKSPACE
      </p>
      <MainNavigation />
      <div className="mt-auto pt-11 max-md:hidden">
        <SidebarNote />
        <button
          className="flex items-center gap-2.75 px-3.25 py-3 text-small text-haze-600"
          onClick={onHelp}
        >
          <CircleHelp size={17} />
          Getting started
        </button>
        <Profile user={user} onLogout={onLogout} loggingOut={loggingOut} />
      </div>
    </aside>
  );
}
function WorkspaceSwitcher() {
  return (
    <div className="mx-0.75 mb-7 flex items-center gap-2.25 rounded-lg border border-edge bg-[#fcfcfc] px-2.25 py-2.75 max-md:hidden">
      <span className="grid size-7.25 place-items-center rounded-[7px] bg-mauve-200 font-semibold text-[#73638d]">
        F
      </span>
      <div>
        <strong className="block text-label font-semibold">
          Personal workspace
        </strong>
        <small className="mt-1 block text-tiny text-muted">
          A space for your best work
        </small>
      </div>
      <ChevronsUpDown size={14} className="ml-auto text-haze-500" />
    </div>
  );
}
function MainNavigation() {
  const router = useRouter();
  return (
    <nav
      aria-label="Main navigation"
      className="max-md:flex max-md:items-center max-md:gap-1.25"
    >
      {navigation.map(({ href, label, icon: Icon }) => {
        const active = router.pathname === href;
        return (
          <Link
            key={href}
            href={href}
            aria-label={label}
            aria-current={active ? "page" : undefined}
            className={cx(
              "my-1 flex items-center gap-2.75 rounded-[7px] px-3.25 py-2.75 text-small font-medium max-md:p-2 max-md:text-label max-sm:gap-1.25",
              active
                ? "bg-mauve-200 text-mauve-900"
                : "text-haze-600 hover:bg-mauve-100",
            )}
          >
            <Icon size={18} strokeWidth={1.65} className="max-md:w-4" />
            <span className="max-sm:hidden">{label}</span>
            {active && (
              <span className="ml-auto size-1.25 rounded-full bg-accent max-md:hidden" />
            )}
          </Link>
        );
      })}
    </nav>
  );
}
function SidebarNote() {
  return (
    <div className="mx-1.25 mb-6.5 border-t border-edge px-2.75 py-4.25">
      <span className="mb-3.5 block text-[26px] text-accent">✳</span>
      <h3 className="text-small font-[550]">Good things take shape.</h3>
      <p className="mt-1.75 mb-3.5 text-label leading-[1.8] text-haze-500">
        A little structure.
        <br />A lot of possibility.
      </p>
      <Link
        href={routes.projects}
        className="flex items-center gap-1.25 text-caption"
      >
        Make room for your ideas <ArrowUpRight size={14} />
      </Link>
    </div>
  );
}
function Profile({
  user,
  onLogout,
  loggingOut,
}: {
  user: User;
  onLogout: () => void;
  loggingOut: boolean;
}) {
  const initials = user.name
    .split(" ")
    .map((part) => part[0])
    .slice(0, 2)
    .join("");
  return (
    <div className="mt-3 flex items-center gap-2.25 border-t border-edge px-1.25 pt-4.5">
      <span className="grid size-8 shrink-0 place-items-center rounded-full border-2 border-white bg-sand text-caption font-semibold text-sand-ink">
        {initials}
      </span>
      <div className="min-w-0">
        <strong className="block text-label font-semibold">{user.name}</strong>
        <small className="mt-1 block max-w-[125px] overflow-hidden text-tiny text-ellipsis text-haze-400">
          {user.email}
        </small>
      </div>
      <IconButton
        className="ml-auto"
        onClick={onLogout}
        disabled={loggingOut}
        aria-label="Sign out"
        title="Sign out"
      >
        <LogOut size={17} />
      </IconButton>
    </div>
  );
}
