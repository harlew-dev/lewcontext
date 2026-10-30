import { cx } from "@/lib/cx";
export function Brand({ light = false }: { light?: boolean }) {
  const bar = cx(
    "block h-1.5 rounded-[1px] max-sm:h-1.25",
    light ? "bg-[#e0d3ef]" : "bg-ink",
  );
  return (
    <div
      className={cx(
        "flex items-center gap-2.5 font-[650] tracking-[-1.6px]",
        light ? "text-[31px] text-mauve-text" : "text-[28px] text-ink",
        "max-md:text-[24px] max-sm:gap-2 max-sm:text-[23px]",
      )}
    >
      <span className="flex h-7.25 w-6.25 -skew-y-12 flex-col gap-1 max-sm:h-6 max-sm:w-5.25 max-sm:gap-0.75">
        <i className={cx(bar, "w-6 max-sm:w-5")} />
        <i className={cx(bar, "w-4.5 max-sm:w-3.75")} />
        <i className={cx(bar, "w-2.25 max-sm:w-2")} />
      </span>
      <span>
        forma<span className="text-[#9180b2]">.</span>
      </span>
    </div>
  );
}
