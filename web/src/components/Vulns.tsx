import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get } from '@/api/client'
import type { PackageVulns, Severity, VulnFinding } from '@/api/types'
import { RelTime } from './Format'

export const severities: Severity[] = ['CRITICAL', 'HIGH', 'MODERATE', 'LOW', 'UNKNOWN']

// Four levels from the three status colours the design system defines —
// amber is reserved for the brand, so it is not borrowed for "moderate".
const severityStyle: Record<Severity, React.CSSProperties> = {
  CRITICAL: { background: 'var(--hlk-error)', color: '#fff', borderColor: 'var(--hlk-error)' },
  HIGH: { background: 'var(--hlk-error-bg)', color: 'var(--hlk-error)', borderColor: 'var(--hlk-error)' },
  MODERATE: { background: 'var(--hlk-warning-bg)', color: 'var(--hlk-text)', borderColor: 'var(--hlk-warning)' },
  LOW: { background: 'transparent', color: 'var(--hlk-info)', borderColor: 'var(--hlk-info)' },
  UNKNOWN: { background: 'transparent', color: 'var(--hlk-text-tertiary)', borderColor: 'var(--hlk-border)' },
}

export function SeverityTag({ severity, score }: { severity: Severity; score?: number | null }) {
  const { t } = useTranslation()
  return (
    <span
      style={{
        ...severityStyle[severity] ?? severityStyle.UNKNOWN,
        display: 'inline-flex', alignItems: 'center', gap: 5, border: '1px solid', borderRadius: 4,
        padding: '0 7px', fontSize: 11, fontWeight: 600, lineHeight: '19px', whiteSpace: 'nowrap',
      }}
    >
      {t(`vulns.sev.${severity}`, severity)}
      {score != null && <span className="hlk-mono" style={{ fontWeight: 500, opacity: 0.85 }}>{score.toFixed(1)}</span>}
    </span>
  )
}

// The id links to OSV, which has the full advisory. A CVE alias is shown
// beside a GHSA id because that is the name most people search for.
export function VulnId({ f }: { f: Pick<VulnFinding, 'id' | 'aliases'> }) {
  const cve = f.id.startsWith('CVE-') ? undefined : f.aliases?.find((a) => a.startsWith('CVE-'))
  return (
    <span style={{ display: 'inline-flex', flexDirection: 'column' }}>
      <a className="hlk-mono" style={{ fontSize: 12 }} href={`https://osv.dev/vulnerability/${encodeURIComponent(f.id)}`} target="_blank" rel="noreferrer">{f.id}</a>
      {cve && <span className="hlk-mono" style={{ fontSize: 11, color: 'var(--hlk-text-tertiary)' }}>{cve}</span>}
    </span>
  )
}

export function packageLabel(f: Pick<VulnFinding, 'format' | 'namespace' | 'name'>) {
  if (!f.namespace) return f.name
  return f.format === 'maven' ? `${f.namespace}:${f.name}` : `${f.namespace}/${f.name}`
}

// One package's findings, for the package drawer. What it says when there
// are none depends on why: "no known vulnerabilities" is only true for a
// package that was actually checked.
export function PackageVulnsSection({ packageId }: { packageId: string }) {
  const { t } = useTranslation()
  const q = useQuery({ queryKey: ['package-vulns', packageId], queryFn: () => get<PackageVulns>(`packages/${packageId}/vulnerabilities`) })
  const v = q.data
  if (!v || v.status === 'disabled') return null
  const note = (text: string) => <p style={{ margin: 0, fontSize: 12.5, color: 'var(--hlk-text-secondary)' }}>{text}</p>
  return (
    <>
      <div className="hlk-section-label" style={{ margin: '20px 0 8px' }}>
        {t('vulns.title', 'Vulnerabilities')}{v.items.length > 0 && ` · ${v.items.length}`}
      </div>
      {v.status === 'notCovered' && note(t('vulns.pkg.notCovered', 'Not checked: OSV has no data for this package. This does not mean it is safe.'))}
      {v.status === 'excluded' && note(t('vulns.pkg.excluded', 'Not checked: vulnerability scanning is turned off for this repository.'))}
      {v.status === 'pending' && note(t('vulns.pkg.pending', 'Not checked yet. New packages are checked within the hour.'))}
      {v.status === 'scanned' && v.items.length === 0 && (
        <p style={{ margin: 0, fontSize: 12.5, color: 'var(--hlk-text-secondary)' }}>
          {t('vulns.pkg.clean', 'No known vulnerabilities.')}{' '}
          {v.scannedAt && <span style={{ color: 'var(--hlk-text-tertiary)' }}>{t('vulns.pkg.checked', 'Checked')} <RelTime value={v.scannedAt} /></span>}
        </p>
      )}
      {v.items.length > 0 && (
        <div style={{ display: 'grid', gap: 10 }}>
          {v.items.map((f) => (
            <div key={f.id} style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '4px 12px', paddingBottom: 10, borderBottom: '1px solid var(--hlk-row)' }}>
              <SeverityTag severity={f.severity} score={f.score} />
              <VulnId f={f} />
              <span />
              <span style={{ fontSize: 13 }}>{f.summary || '—'}</span>
              <span />
              <span style={{ fontSize: 12, color: 'var(--hlk-text-secondary)' }}>
                {f.fixedIn.length > 0
                  ? <>{t('vulns.fixedIn', 'Fixed in')} <span className="hlk-mono">{f.fixedIn.join(', ')}</span></>
                  : t('vulns.noFix', 'No fixed version published')}
              </span>
            </div>
          ))}
        </div>
      )}
    </>
  )
}
