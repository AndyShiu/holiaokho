import { useState } from 'react'
import { CheckOutlined, CopyOutlined } from '@ant-design/icons'
import { App, Tooltip } from 'antd'
import { useTranslation } from 'react-i18next'

export function copyText(text: string) {
  if (navigator.clipboard?.writeText) return navigator.clipboard.writeText(text)
  const ta = document.createElement('textarea')
  ta.value = text
  document.body.appendChild(ta)
  ta.select()
  document.execCommand('copy')
  ta.remove()
  return Promise.resolve()
}

export function Copyable({ text, display, mono = true, maxWidth, style, block }: { text?: string | null; display?: string; mono?: boolean; maxWidth?: number | string; style?: React.CSSProperties; block?: boolean }) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const [done, setDone] = useState(false)
  const value = text ?? ''
  const onCopy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    await copyText(value)
    setDone(true)
    message.success(t('common.copied', 'Copied'))
    setTimeout(() => setDone(false), 1500)
  }
  return (
    <Tooltip title={value.length > 40 ? value : undefined}>
      <span
        onClick={onCopy}
        style={{ display: block ? 'flex' : 'inline-flex', alignItems: 'center', gap: 6, cursor: 'pointer', fontFamily: mono ? 'var(--hlk-mono)' : undefined, maxWidth, minWidth: 0, ...style }}
      >
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', minWidth: 0 }}>{display ?? value ?? '—'}</span>
        {done ? <CheckOutlined style={{ color: 'var(--hlk-success)', fontSize: 13, flex: 'none' }} /> : <CopyOutlined style={{ color: 'var(--hlk-text-tertiary)', fontSize: 13, flex: 'none' }} />}
      </span>
    </Tooltip>
  )
}

export function CodeBlock({ code, title, filename, span2 }: { code: string; title?: string; filename?: string; span2?: boolean }) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  return (
    <div className="hlk-codecard" style={span2 ? { gridColumn: 'span 2' } : undefined}>
      {(title || filename) && (
        <div className="hlk-codecard-head">
          <span style={{ fontWeight: 600, fontSize: 13 }}>{title}</span>
          {filename && <span className="hlk-mono" style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{filename}</span>}
        </div>
      )}
      <div style={{ position: 'relative' }}>
        <pre className="hlk-code">{code}</pre>
        <button
          className="hlk-copybtn"
          onClick={async () => {
            await copyText(code)
            message.success(t('common.copied', 'Copied'))
          }}
        >
          {t('common.copy', 'Copy')}
        </button>
      </div>
    </div>
  )
}
