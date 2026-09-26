/**
 * Heliostat mark: rays converging on one point, the same shapes as `public/favicon.svg`.
 * Brand colors are fixed so the mark looks identical in light and dark themes.
 */
export function LogoMark({ size = 32, x, y }: { size?: number; x?: number; y?: number }) {
  return (
    <svg x={x} y={y} width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
      <rect x="3" y="3" width="28" height="28" fill="#3b5bdb" />
      <rect x="1" y="1" width="28" height="28" fill="#ffd83d" stroke="#111" strokeWidth="2" />
      <circle cx="15" cy="10" r="3.4" fill="#111" />
      <path
        d="M5 25 L15 10 M25 25 L15 10 M15 25 L15 10"
        stroke="#111"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </svg>
  );
}
