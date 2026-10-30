import type { ReactNode } from "react";
import { cx } from "@/lib/cx";
// Callers pass className for spacing only; size and colour come from variants
// so overrides never compete with the base utilities.
const eyebrows = {
  page: "text-caption tracking-[1.6px] text-mauve-700 max-sm:text-micro max-sm:tracking-[1.2px]",
  form: "text-micro tracking-[1.3px] text-mauve-700",
  inverse: "text-tiny tracking-[1.6px] text-[#b3a4c1]",
};
export function Eyebrow({
  children,
  variant = "page",
  className,
}: {
  children: ReactNode;
  variant?: keyof typeof eyebrows;
  className?: string;
}) {
  return (
    <span className={cx("block font-semibold", eyebrows[variant], className)}>
      {children}
    </span>
  );
}
const descriptions = {
  body: "text-body max-sm:text-small",
  small: "text-label max-sm:text-caption",
};
export function Description({
  children,
  size = "body",
  className,
}: {
  children: ReactNode;
  size?: keyof typeof descriptions;
  className?: string;
}) {
  return (
    <p
      className={cx("leading-[1.7] text-muted", descriptions[size], className)}
    >
      {children}
    </p>
  );
}
export function PageHeading({
  eyebrow,
  title,
  description,
  action,
}: {
  eyebrow?: string;
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <header className="mb-7.25 flex items-center justify-between gap-5 max-sm:mb-6 max-sm:block">
      <div>
        {eyebrow && <Eyebrow className="mb-2.75">{eyebrow}</Eyebrow>}
        <h1 className="max-md:text-[26px]">{title}</h1>
        <Description className="mt-1.75">{description}</Description>
      </div>
      {action && <div className="shrink-0 pt-3 max-sm:pt-4">{action}</div>}
    </header>
  );
}
export function SectionHeading({
  title,
  description,
  action,
  compact = false,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  compact?: boolean;
}) {
  return (
    <div className="mb-4.5 flex items-center justify-between gap-3.75 max-sm:items-start">
      <div>
        <h2 className={cx(compact && "text-copy", "max-sm:text-[16px]")}>
          {title}
        </h2>
        {description && (
          <Description size="small" className="mt-1.25">
            {description}
          </Description>
        )}
      </div>
      {action}
    </div>
  );
}
