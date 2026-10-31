import { useEffect, useRef, useState } from "react";
import { MagnifyingGlassIcon } from "@heroicons/react/16/solid";
import { cleanSearch } from "@/lib/pagination";
import { Input, InputGroup } from "./input";
// Search box that updates the URL once typing pauses. It follows the URL when
// that changes elsewhere (back/forward), without discarding text being typed.
// `label` names it for screen readers and, with "…", is the placeholder.
export function SearchField({
  label,
  value,
  onSearch,
}: {
  label: string;
  value: string;
  onSearch: (q: string) => void;
}) {
  const [draft, setDraft] = useState(value);
  const [shown, setShown] = useState(value);
  // The last search this field sent; the URL catching up to it is no reason
  // to overwrite the draft.
  const [sent, setSent] = useState(value);
  if (value !== shown) {
    setShown(value);
    if (value !== sent) {
      setDraft(value);
      setSent(value);
    }
  }
  const search = useRef(onSearch);
  useEffect(() => {
    search.current = onSearch;
  });
  useEffect(() => {
    const q = cleanSearch(draft);
    if (q === value) return;
    const timer = setTimeout(() => {
      setSent(q);
      search.current(q);
    }, 300);
    return () => clearTimeout(timer);
  }, [draft, value]);
  return (
    <InputGroup>
      <MagnifyingGlassIcon />
      <Input
        type="search"
        aria-label={label}
        placeholder={`${label}…`}
        maxLength={100}
        value={draft}
        onChange={(e) => setDraft(e.currentTarget.value)}
      />
    </InputGroup>
  );
}
