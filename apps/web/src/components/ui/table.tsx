import {
  createContext,
  useContext,
  useState,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from "react";
import {
  ChevronDownIcon,
  ChevronUpDownIcon,
  ChevronUpIcon,
} from "@heroicons/react/16/solid";
import { cx } from "@/lib/cx";
import type { SortDirection } from "@/lib/pagination";
import { Link } from "./link";
const TableContext = createContext({
  bleed: false,
  dense: false,
  grid: false,
  striped: false,
});
type TableOptions = {
  bleed?: boolean;
  dense?: boolean;
  grid?: boolean;
  striped?: boolean;
};
export function Table({
  bleed = false,
  dense = false,
  grid = false,
  striped = false,
  className,
  children,
  ...props
}: TableOptions & ComponentPropsWithoutRef<"div">) {
  return (
    <TableContext.Provider value={{ bleed, dense, grid, striped }}>
      <div className="flow-root">
        <div
          {...props}
          className={cx(
            className,
            "-mx-(--gutter) overflow-x-auto whitespace-nowrap",
          )}
        >
          <div
            className={cx(
              "inline-block min-w-full align-middle",
              !bleed && "sm:px-(--gutter)",
            )}
          >
            <table className="min-w-full text-left text-sm/6 text-zinc-950 dark:text-white">
              {children}
            </table>
          </div>
        </div>
      </div>
    </TableContext.Provider>
  );
}
export function TableHead({
  className,
  ...props
}: ComponentPropsWithoutRef<"thead">) {
  return (
    <thead
      {...props}
      className={cx(className, "text-zinc-500 dark:text-zinc-400")}
    />
  );
}
export function TableBody(props: ComponentPropsWithoutRef<"tbody">) {
  return <tbody {...props} />;
}
// A row with `href` links every cell to it; `title` names the link.
const TableRowContext = createContext<{
  href?: string;
  target?: string;
  title?: string;
}>({});
export function TableRow({
  href,
  target,
  title,
  className,
  ...props
}: {
  href?: string;
  target?: string;
  title?: string;
} & ComponentPropsWithoutRef<"tr">) {
  const { striped } = useContext(TableContext);
  return (
    <TableRowContext.Provider value={{ href, target, title }}>
      <tr
        {...props}
        className={cx(
          className,
          href &&
            "has-[[data-row-link][data-focus]]:outline-2 has-[[data-row-link][data-focus]]:-outline-offset-2 has-[[data-row-link][data-focus]]:outline-blue-500 dark:focus-within:bg-white/2.5",
          striped && "even:bg-zinc-950/2.5 dark:even:bg-white/2.5",
          href && striped && "hover:bg-zinc-950/5 dark:hover:bg-white/5",
          href && !striped && "hover:bg-zinc-950/2.5 dark:hover:bg-white/2.5",
        )}
      />
    </TableRowContext.Provider>
  );
}
// With `href` the header links to that sort and reports `sort` as aria-sort.
export function TableHeader({
  href,
  sort,
  shallow,
  className,
  children,
  ...props
}: {
  href?: string;
  sort?: SortDirection;
  shallow?: boolean;
} & ComponentPropsWithoutRef<"th">) {
  const { bleed, grid } = useContext(TableContext);
  return (
    <th
      aria-sort={href ? (sort ?? "none") : undefined}
      {...props}
      className={cx(
        className,
        "border-b border-b-zinc-950/10 px-4 py-2 font-medium first:pl-(--gutter,--spacing(2)) last:pr-(--gutter,--spacing(2)) dark:border-b-white/10",
        grid &&
          "border-l border-l-zinc-950/5 first:border-l-0 dark:border-l-white/5",
        !bleed && "sm:first:pl-1 sm:last:pr-1",
      )}
    >
      {href ? (
        <SortLink href={href} sort={sort} shallow={shallow}>
          {children}
        </SortLink>
      ) : (
        children
      )}
    </th>
  );
}
const sortIcons = {
  ascending: ChevronUpIcon,
  descending: ChevronDownIcon,
};
function SortLink({
  href,
  sort,
  shallow,
  children,
}: {
  href: string;
  sort?: SortDirection;
  shallow?: boolean;
  children: ReactNode;
}) {
  const Icon = sort ? sortIcons[sort] : ChevronUpDownIcon;
  return (
    <Link
      href={href}
      shallow={shallow}
      scroll={false}
      className={cx(
        "group inline-flex cursor-default items-center gap-1 rounded-sm focus:not-data-focus:outline-hidden data-focus:outline-2 data-focus:outline-offset-2 data-focus:outline-blue-500",
        "data-hover:text-zinc-950 dark:data-hover:text-white",
        sort && "text-zinc-950 dark:text-white",
      )}
    >
      {children}
      <Icon
        className={cx(
          "size-4 shrink-0",
          sort
            ? "fill-zinc-500 dark:fill-zinc-400"
            : "fill-zinc-400 group-data-hover:fill-zinc-500 dark:fill-zinc-500",
        )}
      />
    </Link>
  );
}
export function TableCell({
  className,
  children,
  ...props
}: ComponentPropsWithoutRef<"td">) {
  const { bleed, dense, grid, striped } = useContext(TableContext);
  const { href, target, title } = useContext(TableRowContext);
  const [cell, setCell] = useState<HTMLElement | null>(null);
  return (
    <td
      ref={href ? setCell : undefined}
      {...props}
      className={cx(
        className,
        "relative px-4 first:pl-(--gutter,--spacing(2)) last:pr-(--gutter,--spacing(2))",
        !striped && "border-b border-zinc-950/5 dark:border-white/5",
        grid &&
          "border-l border-l-zinc-950/5 first:border-l-0 dark:border-l-white/5",
        dense ? "py-2.5" : "py-4",
        !bleed && "sm:first:pl-1 sm:last:pr-1",
      )}
    >
      {href && (
        // Every cell is clickable; only the first is in the tab order.
        <Link
          data-row-link
          href={href}
          target={target}
          aria-label={title}
          tabIndex={cell?.previousElementSibling === null ? 0 : -1}
          className="absolute inset-0 focus:outline-hidden"
        />
      )}
      {children}
    </td>
  );
}
