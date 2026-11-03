import { useMemo } from "react";
import qrcode from "qrcode-generator";
import { cx } from "@/lib/cx";
// A QR code drawn in the page as one SVG path: no image URL, no HTML string,
// so the content security policy needs nothing extra. Always dark on white
// (scanners need the contrast, in dark mode too), with the standard quiet
// zone of 4 modules around it.
export function QrCode({
  value,
  label,
  className,
}: {
  value: string;
  // What the code is, for screen readers.
  label: string;
  className?: string;
}) {
  const { size, path } = useMemo(() => qrPath(value), [value]);
  const box = size + 2 * quietZone;
  return (
    <svg
      role="img"
      aria-label={label}
      viewBox={`${-quietZone} ${-quietZone} ${box} ${box}`}
      shapeRendering="crispEdges"
      className={cx(className, "size-48 rounded-lg forced-color-adjust-none")}
    >
      <rect
        x={-quietZone}
        y={-quietZone}
        width={box}
        height={box}
        className="fill-white"
      />
      <path d={path} className="fill-zinc-950" />
    </svg>
  );
}
const quietZone = 4;
// The code's modules as one path of unit squares, and how many there are per
// side. Error correction M, as authenticator setup screens use.
export function qrPath(value: string): { size: number; path: string } {
  const qr = qrcode(0, "M");
  qr.addData(value);
  qr.make();
  const size = qr.getModuleCount();
  let path = "";
  for (let row = 0; row < size; row++)
    for (let col = 0; col < size; col++)
      if (qr.isDark(row, col)) path += `M${col} ${row}h1v1h-1z`;
  return { size, path };
}
