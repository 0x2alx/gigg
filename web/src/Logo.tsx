// The gigg ship mark (assets/brand/svg/logo-mono.svg) as one colour in currentColor, so CSS can
// tint it with the theme accent. The kanban columns and the nose light are cut out of the hull
// with a mask; the mask id is per instance so two marks on one page don't share it.
import { useId } from "react";

// compact: for small sizes (sidebar): no fins or nose light, wider columns, so it stays a clear ship.
export function LogoMark({ className, title, compact }: { className?: string; title?: string; compact?: boolean }) {
  const id = "gigg-cut-" + useId().replace(/[^a-zA-Z0-9_-]/g, "");
  const fin = "M141,151 C149,152 151,168 149,178 L131,210 C127,196 128,172 132,161 C134,155 137,151 141,151 Z";
  const flame = "M135,299 Q135,297 137,297 L157,297 Q159,297 159,299 C157,328 152,348 146,367 C141,348 136,328 135,299 Z";
  const mirror = "translate(450,0) scale(-1,1)";
  return (
    <svg className={className} viewBox="94 66 262 304" fill="currentColor" role={title ? "img" : undefined} aria-hidden={title ? undefined : true} aria-label={title}>
      <defs>
        <mask id={id} maskUnits="userSpaceOnUse" x="0" y="0" width="450" height="450">
          <path d="M225,74 L350,295 L275,283 L262,297 L250,313 L200,313 L188,297 L175,283 L100,295 Z" fill="white" stroke="white" strokeWidth="12" strokeLinejoin="round" />
          {compact ? (
            <>
              <rect x="170" y="190" width="26" height="82" rx="13" fill="black" />
              <rect x="212" y="158" width="26" height="114" rx="13" fill="black" />
              <rect x="254" y="190" width="26" height="82" rx="13" fill="black" />
            </>
          ) : (
            <>
              <rect x="175" y="193" width="18" height="78" rx="9" fill="black" />
              <rect x="216" y="163" width="18" height="108" rx="9" fill="black" />
              <rect x="257" y="193" width="18" height="78" rx="9" fill="black" />
              <path d="M225,104 L238,131 L212,131 Z" fill="black" stroke="black" strokeWidth="6" strokeLinejoin="round" />
            </>
          )}
        </mask>
      </defs>
      <rect x="0" y="0" width="450" height="450" mask={`url(#${id})`} />
      {!compact && <path d={fin} />}
      {!compact && <path d={fin} transform={mirror} />}
      <path d={flame} />
      <path d={flame} transform={mirror} />
    </svg>
  );
}
