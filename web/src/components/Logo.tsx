import { AMBER, INK } from '@/theme/tokens'

// Symbol from design/README.md: "广" roof + stacked goods.
export function LogoMark({ size = 24, ink = INK, top = AMBER }: { size?: number; ink?: string; top?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden>
      <path d="M4 13 L16 4 L28 13" fill="none" stroke={ink} strokeWidth={3.5} strokeLinecap="round" strokeLinejoin="round" />
      <rect x="12.5" y="14.5" width="7" height="6" rx="1" fill={top} />
      <rect x="7.5" y="22" width="7" height="6" rx="1" fill={ink} />
      <rect x="17.5" y="22" width="7" height="6" rx="1" fill={ink} />
    </svg>
  )
}

export function Wordmark({ size = 15, color }: { size?: number; color?: string }) {
  return (
    <span style={{ fontFamily: '"DM Sans", system-ui, sans-serif', fontWeight: 700, fontSize: size, letterSpacing: '-0.03em', color, lineHeight: 1 }}>
      Holiaokho
    </span>
  )
}
