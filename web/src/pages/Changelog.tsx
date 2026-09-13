import { useTranslation } from 'react-i18next'
import { Tag } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { get } from '@/api/client'
import { releases, pick } from '@/data/changelog'

interface Status { version?: string }

const kindColour: Record<string, string> = { added: 'green', fixed: 'gold', changed: 'blue' }

export default function Changelog() {
  const { t, i18n } = useTranslation()
  const status = useQuery({ queryKey: ['status'], queryFn: () => get<Status>('status') })
  const running = status.data?.version

  const kindLabel: Record<string, string> = {
    added: t('changelog.added', 'Added'),
    fixed: t('changelog.fixed', 'Fixed'),
    changed: t('changelog.changed', 'Changed'),
  }

  return (
    <div style={{ maxWidth: 760, margin: '0 auto' }}>
      <h1 style={{ fontSize: 22, fontWeight: 600, margin: '0 0 4px' }}>{t('changelog.title', 'Release notes')}</h1>
      <p style={{ fontSize: 13, color: 'var(--hlk-text-tertiary)', margin: '0 0 24px' }}>
        {t('changelog.hint', 'What changed in each release.')}
      </p>

      {releases.map((r) => (
        <section key={r.version} className="hlk-card" style={{ marginBottom: 16 }}>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, flexWrap: 'wrap', marginBottom: r.headline ? 10 : 14 }}>
            <span className="hlk-mono" style={{ fontSize: 16, fontWeight: 600 }}>v{r.version}</span>
            {running === r.version && (
              <Tag color="green" style={{ marginInlineEnd: 0 }}>{t('changelog.running', 'running now')}</Tag>
            )}
            <span className="hlk-mono" style={{ fontSize: 12, color: 'var(--hlk-text-tertiary)', marginLeft: 'auto' }}>{r.date}</span>
          </div>
          {r.headline && (
            <p style={{ fontSize: 13.5, lineHeight: 1.7, color: 'var(--hlk-text-secondary)', margin: '0 0 14px' }}>
              {pick(r.headline, i18n.language)}
            </p>
          )}
          <ul style={{ margin: 0, padding: 0, listStyle: 'none', display: 'grid', gap: 10 }}>
            {r.changes.map((c, i) => (
              <li key={i} style={{ display: 'grid', gridTemplateColumns: '68px 1fr', gap: 10, alignItems: 'start' }}>
                <Tag color={kindColour[c.kind]} style={{ marginInlineEnd: 0, fontSize: 11, textAlign: 'center', width: '100%' }}>
                  {kindLabel[c.kind]}
                </Tag>
                <span style={{ fontSize: 13.5, lineHeight: 1.7 }}>{pick(c.text, i18n.language)}</span>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}
