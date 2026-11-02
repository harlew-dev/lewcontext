import { forwardRef, type ButtonHTMLAttributes, type Ref } from "react";
import { useInteractive } from "@/lib/interactive";
import { Link } from "./link";
type ClickableProps = { className?: string } & (
  | ({ href: string } & Omit<
      React.ComponentPropsWithoutRef<typeof Link>,
      "href"
    >)
  | ({ href?: never } & ButtonHTMLAttributes<HTMLButtonElement>)
);
// A link when given `href`, otherwise a button; both report data-hover,
// data-active and data-focus for the item styles (sidebar, navbar).
export const Clickable = forwardRef<HTMLElement, ClickableProps>(
  function Clickable(props, ref) {
    const button = props as ButtonHTMLAttributes<HTMLButtonElement>;
    const interactive = useInteractive<HTMLButtonElement>(button);
    if (props.href !== undefined)
      return (
        <Link
          {...props}
          href={props.href}
          ref={ref as Ref<HTMLAnchorElement>}
        />
      );
    return (
      <button
        type="button"
        {...button}
        {...interactive}
        ref={ref as Ref<HTMLButtonElement>}
        className={`cursor-default ${props.className ?? ""}`}
      />
    );
  },
);
