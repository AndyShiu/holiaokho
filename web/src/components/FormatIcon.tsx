import { formatInfo } from '@/theme/tokens'

export function FormatIcon({ format, size = 20 }: { format: string; size?: number }) {
  const f = formatInfo(format)
  const radius = size <= 16 ? 4 : size <= 20 ? 5 : 7
  const font = size <= 16 ? 8 : size <= 20 ? 9 : 10
  return (
    <span
      title={f.label}
      style={{
        display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: size, height: size, borderRadius: radius,
        background: f.color, color: f.fg ?? '#fff', fontFamily: 'var(--hlk-mono)', fontWeight: 700, fontSize: font, letterSpacing: '-0.02em', flex: 'none',
      }}
    >
      {f.abbr}
    </span>
  )
}

export function FormatLabel({ format, size = 18 }: { format: string; size?: number }) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
      <FormatIcon format={format} size={size} />
      <span>{formatInfo(format).label}</span>
    </span>
  )
}
