import dayjs from 'dayjs'
import { Tooltip } from 'antd'
import { useTranslation } from 'react-i18next'

export function fmtBytes(n?: number | null): string {
  if (n === undefined || n === null) return '—'
  if (n < 1024) return `${n} B`
  const u = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024
    i++
  }
  return `${v < 10 ? v.toFixed(2) : v < 100 ? v.toFixed(1) : Math.round(v)} ${u[i]}`
}

export function RelTime({ value, empty = '—' }: { value?: string | null; empty?: string }) {
  if (!value) return <span style={{ color: 'var(--hlk-text-tertiary)' }}>{empty}</span>
  const d = dayjs(value)
  if (!d.isValid() || d.year() < 1971) return <span style={{ color: 'var(--hlk-text-tertiary)' }}>{empty}</span>
  return (
    <Tooltip title={d.format('YYYY-MM-DD HH:mm:ss')}>
      <span style={{ whiteSpace: 'nowrap' }}>{d.fromNow()}</span>
    </Tooltip>
  )
}

export function AbsTime({ value }: { value?: string | null }) {
  if (!value) return <span>—</span>
  return <span className="hlk-mono" style={{ fontSize: 12 }}>{dayjs(value).format('YYYY-MM-DD HH:mm:ss')}</span>
}

export function Num({ value }: { value?: number | null }) {
  const { i18n } = useTranslation()
  return <span style={{ fontVariantNumeric: 'tabular-nums' }}>{value === undefined || value === null ? '—' : value.toLocaleString(i18n.language)}</span>
}

export function StatusDot({ status, size = 8 }: { status: 'success' | 'warning' | 'error' | 'info' | 'idle' | 'running'; size?: number }) {
  const color = { success: 'var(--hlk-success)', warning: 'var(--hlk-warning)', error: 'var(--hlk-error)', info: 'var(--hlk-info)', running: 'var(--hlk-info)', idle: 'var(--hlk-text-quaternary)' }[status]
  return <span className={status === 'running' ? 'hlk-pulse' : undefined} style={{ display: 'inline-block', width: size, height: size, borderRadius: '50%', background: color, flex: 'none' }} />
}
