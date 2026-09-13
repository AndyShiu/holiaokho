import { type ReactNode, useState } from 'react'
import { Alert, Button, Empty, Input, Modal, Space, Typography } from 'antd'
import { ExclamationCircleFilled } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { ApiError } from '@/api/client'

export function PageHeader({ title, sub, extra, count }: { title: ReactNode; sub?: ReactNode; extra?: ReactNode; count?: number | string }) {
  return (
    <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 16, marginBottom: 20, flexWrap: 'wrap' }}>
      <div style={{ minWidth: 0 }}>
        <h1 style={{ margin: 0, fontSize: 22, fontWeight: 600, lineHeight: 1.25, display: 'flex', alignItems: 'baseline', gap: 10 }}>
          {title}
          {count !== undefined && <span className="hlk-mono" style={{ fontSize: 12, fontWeight: 400, color: 'var(--hlk-text-tertiary)' }}>{count}</span>}
        </h1>
        {sub && <div style={{ color: 'var(--hlk-text-secondary)', fontSize: 13, marginTop: 4 }}>{sub}</div>}
      </div>
      {extra && <Space wrap>{extra}</Space>}
    </div>
  )
}

export function Section({ title, children, extra, style }: { title?: ReactNode; children: ReactNode; extra?: ReactNode; style?: React.CSSProperties }) {
  return (
    <div className="hlk-card" style={style}>
      {(title || extra) && (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 14 }}>
          <div className="hlk-section-label">{title}</div>
          {extra}
        </div>
      )}
      {children}
    </div>
  )
}

/** Localised API error; falls back to the server message and shows the code. */
export function useErrorText() {
  const { t } = useTranslation()
  return (err: unknown): string => {
    if (err instanceof ApiError) {
      const key = `errors.${err.code}`
      const params: Record<string, unknown> = {}
      err.params.forEach((p, i) => (params[`p${i}`] = p))
      const localised = t(key, { ...params, defaultValue: '' })
      if (localised) return localised
      return `${err.message} (${err.code})`
    }
    if (err instanceof Error) return err.message
    return String(err)
  }
}

export function ErrorAlert({ error, style }: { error: unknown; style?: React.CSSProperties }) {
  const text = useErrorText()
  if (!error) return null
  return <Alert type="error" showIcon message={text(error)} style={{ marginBottom: 16, ...style }} />
}

export function EmptyState({ title, hint, action }: { title: ReactNode; hint?: ReactNode; action?: ReactNode }) {
  return (
    <div style={{ padding: '48px 24px', textAlign: 'center' }}>
      <Empty description={<div><div style={{ fontWeight: 500 }}>{title}</div>{hint && <div style={{ color: 'var(--hlk-text-secondary)', fontSize: 13, marginTop: 4 }}>{hint}</div>}</div>} image={Empty.PRESENTED_IMAGE_SIMPLE}>
        {action}
      </Empty>
    </div>
  )
}

/** Destructive confirmation requiring the user to type the resource name. */
export function ConfirmDelete({
  open, name, title, description, confirmLabel, onCancel, onConfirm, loading, typeToConfirm = true,
}: { open: boolean; name: string; title: ReactNode; description?: ReactNode; confirmLabel?: string; onCancel: () => void; onConfirm: () => Promise<void> | void; loading?: boolean; typeToConfirm?: boolean }) {
  const { t } = useTranslation()
  const [typed, setTyped] = useState('')
  const ok = !typeToConfirm || typed === name
  return (
    <Modal open={open} onCancel={onCancel} footer={null} width={480} destroyOnClose afterClose={() => setTyped('')}>
      <div style={{ display: 'flex', gap: 14, alignItems: 'flex-start' }}>
        <div style={{ width: 36, height: 36, borderRadius: '50%', background: 'var(--hlk-error-bg)', color: 'var(--hlk-error)', display: 'grid', placeItems: 'center', flex: 'none', fontSize: 18 }}>
          <ExclamationCircleFilled />
        </div>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 16, fontWeight: 600, marginBottom: 6 }}>{title}</div>
          {description && <div style={{ fontSize: 13, color: 'var(--hlk-text-secondary)', lineHeight: 1.6 }}>{description}</div>}
          {typeToConfirm && (
            <div style={{ marginTop: 16 }}>
              <div style={{ fontSize: 12, marginBottom: 6 }}>
                {t('common.typeToConfirm', 'Type')} <Typography.Text code>{name}</Typography.Text> {t('common.typeToConfirmSuffix', 'to confirm')}
              </div>
              <Input value={typed} onChange={(e) => setTyped(e.target.value)} className="hlk-mono" autoFocus onPressEnter={() => ok && onConfirm()} />
            </div>
          )}
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 20 }}>
            <Button onClick={onCancel}>{t('common.cancel', 'Cancel')}</Button>
            <Button danger type="primary" disabled={!ok} loading={loading} onClick={() => onConfirm()}>
              {confirmLabel ?? t('common.delete', 'Delete')}
            </Button>
          </div>
        </div>
      </div>
    </Modal>
  )
}

export function KV({ items, labelWidth = 120 }: { items: [ReactNode, ReactNode][]; labelWidth?: number }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: `${labelWidth}px 1fr`, rowGap: 10, columnGap: 12, fontSize: 12.5 }}>
      {items.map(([k, v], i) => (
        <div key={i} style={{ display: 'contents' }}>
          <div style={{ color: 'var(--hlk-text-secondary)', fontSize: 12 }}>{k}</div>
          <div style={{ wordBreak: 'break-all', minWidth: 0 }}>{v}</div>
        </div>
      ))}
    </div>
  )
}

export function SecretHint() {
  const { t } = useTranslation()
  return <span style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{t('common.secretKeep', 'Leave empty (or ***) to keep the stored value')}</span>
}
