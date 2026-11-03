import { useSearchParams } from "react-router";
import { Navbar, NavbarItem, NavbarSection } from "@/components/ui/navbar";
// Tabs of one page, kept in its `?tab=` (the first tab has none), so back
// and forward walk through them and a link opens one. Choosing a tab drops
// the other parameters: each tab has its own filters.
export function PageTabs<T extends string>({
  label,
  tabs,
  current,
  className = "mt-4",
}: {
  label: string;
  tabs: readonly { id: T; label: string }[];
  current: T;
  className?: string;
}) {
  const hrefFor = (id: T) => (id === tabs[0].id ? "?" : `?tab=${id}`);
  return (
    <Navbar aria-label={label} className={className}>
      <NavbarSection className="-ml-2">
        {tabs.map((tab) => (
          <NavbarItem
            key={tab.id}
            href={hrefFor(tab.id)}
            current={tab.id === current}
            aria-current={tab.id === current ? "page" : undefined}
          >
            {tab.label}
          </NavbarItem>
        ))}
      </NavbarSection>
    </Navbar>
  );
}
// The tab in a page's URL, or the first when it names none of `tabs`.
export function useTab<T extends string>(tabs: readonly { id: T }[]): T {
  const [search] = useSearchParams();
  const tab = search.get("tab");
  return tabs.find((t) => t.id === tab)?.id ?? tabs[0].id;
}
