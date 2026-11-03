import { ChevronLeftIcon } from "@heroicons/react/16/solid";
import { Link } from "@/components/ui/link";
// The link above a page's heading to the list it belongs to ("Projects",
// "Members"): pages reached from a list have it, in place of a Back button.
export function BackLink({
  href,
  children,
}: {
  href: string;
  children: string;
}) {
  return (
    <Link
      href={href}
      className="inline-flex items-center gap-2 text-sm/6 text-zinc-500 dark:text-zinc-400"
    >
      <ChevronLeftIcon className="size-4 fill-zinc-400 dark:fill-zinc-500" />
      {children}
    </Link>
  );
}
