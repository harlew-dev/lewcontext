import { Fragment, type Key, type ReactNode } from "react";
import { cx } from "@/lib/cx";
import { pageRange, type Page, type Pages } from "@/lib/pagination";
import { Button } from "./button";
import { EmptyState, ListSkeleton } from "./feedback";
import {
  Pagination,
  PaginationGap,
  PaginationList,
  PaginationNext,
  PaginationPage,
  PaginationPrevious,
} from "./pagination";
import { Table, TableBody, TableHead } from "./table";
type PaginatedTableProps<T> = {
  // The current page; undefined until it loads, or when it failed to load.
  page: Page<T> | undefined;
  // A page is loading: a skeleton before the first, otherwise the previous
  // page stays visible, dimmed.
  loading?: boolean;
  // Plural name of the rows, e.g. "Projects", for accessible labels.
  label: string;
  head: ReactNode;
  row: (item: T) => ReactNode;
  rowKey: (item: T) => Key;
  // URL of a page number.
  href: (page: number) => string;
  // Shown when nothing matches at all.
  empty: ReactNode;
  className?: string;
};
// A table of one API page with a page selector below it. Loading shows a
// skeleton; failures are left to the page's notices, which offer a retry.
export function PaginatedTable<T>({
  page,
  loading = false,
  label,
  head,
  row,
  rowKey,
  href,
  empty,
  className,
}: PaginatedTableProps<T>) {
  if (!page)
    return loading ? (
      <ListSkeleton label={`Loading ${label.toLowerCase()}`} />
    ) : null;
  if (page.pages.items === 0) return empty;
  return (
    <div
      aria-busy={loading || undefined}
      className={cx(
        className,
        "transition-opacity motion-reduce:transition-none",
        loading && "opacity-60",
      )}
    >
      {page.data.length ? (
        <Table>
          <TableHead>{head}</TableHead>
          <TableBody>
            {page.data.map((item) => (
              <Fragment key={rowKey(item)}>{row(item)}</Fragment>
            ))}
          </TableBody>
        </Table>
      ) : (
        <EmptyState
          title={`There’s nothing on page ${page.pages.current}`}
          description={`${label} end at page ${page.pages.total}.`}
          action={
            <Button href={href(page.pages.total)}>Go to last page</Button>
          }
        />
      )}
      <PageSelector pages={page.pages} label={label} href={href} />
    </div>
  );
}
function PageSelector({
  pages,
  label,
  href,
}: { pages: Pages } & Pick<PaginatedTableProps<unknown>, "label" | "href">) {
  if (pages.total < 2) return null;
  // From past the end, "Previous" goes to the last page.
  const previous = Math.min(pages.current - 1, pages.total);
  return (
    <Pagination aria-label={`${label} pages`} className="mt-6">
      <PaginationPrevious href={previous >= 1 ? href(previous) : null} />
      <PaginationList>
        {pageRange(pages).map((item, index) =>
          item === "gap" ? (
            <PaginationGap key={`gap-${index}`} />
          ) : (
            <PaginationPage
              key={item}
              href={href(item)}
              current={item === pages.current}
            >
              {item}
            </PaginationPage>
          ),
        )}
      </PaginationList>
      <PaginationNext
        href={pages.current < pages.total ? href(pages.current + 1) : null}
      />
    </Pagination>
  );
}
