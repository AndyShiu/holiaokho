import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { get } from '@/api/client'
import { LogoMark, Wordmark } from '@/components/Logo'
import { AMBER } from '@/theme/tokens'

interface Status { version?: string }

function Strength({ title, body }: { title: string; body: string }) {
  return (
    <div className="hlk-card">
      <h3 style={{ fontSize: 15, fontWeight: 600, margin: '0 0 6px' }}>{title}</h3>
      <p style={{ fontSize: 13, lineHeight: 1.7, color: 'var(--hlk-text-secondary)', margin: 0 }}>{body}</p>
    </div>
  )
}

export default function About() {
  const { t } = useTranslation()
  const status = useQuery({ queryKey: ['status'], queryFn: () => get<Status>('status') })

  const strengths = [
    {
      title: t('about.s1Title', 'One address, every format'),
      body: t('about.s1Body', 'Maven, npm, Docker/OCI, PyPI, NuGet, Go, Helm and twenty more. One server instead of one per language, and one hostname for everyone to remember.'),
    },
    {
      title: t('about.s2Title', 'Light enough to forget about'),
      body: t('about.s2Body', 'A single executable with no JVM, no plugins and no application server. It starts serving in seconds and runs comfortably in a few hundred megabytes.'),
    },
    {
      title: t('about.s3Title', 'Migrating does not hurt'),
      body: t('about.s3Body', 'Paths, API and connector ports follow Nexus conventions. A real cut-over ran on the same host and the same ports with not one line of CI configuration changed.'),
    },
    {
      title: t('about.s4Title', 'Keeps shipping when upstream breaks'),
      body: t('about.s4Body', 'Cached artefacts are served even while the upstream registry is down, so a bad afternoon at a public mirror does not stop your builds. Repeated failures block that upstream automatically instead of making every request wait for a timeout.'),
    },
    {
      title: t('about.s5Title', 'Stores one copy of anything'),
      body: t('about.s5Body', 'Storage is content-addressed: an identical file referenced by ten repositories occupies the disk once. Deleting a repository frees only what nothing else still needs.'),
    },
    {
      title: t('about.s6Title', 'Permissions you can explain'),
      body: t('about.s6Body', 'Roles are written as targets times actions, down to a single repository or a single format. Content selectors narrow that further by path, so a team can publish to its own namespace and nowhere else.'),
    },
    {
      title: t('about.s7Title', 'An interface for people'),
      body: t('about.s7Body', 'Traditional Chinese, Simplified Chinese, English, Japanese and Korean, all first-class. Dark mode, keyboard search, and a layout that survives a laptop screen.'),
    },
    {
      title: t('about.s8Title', 'Yours to keep'),
      body: t('about.s8Body', 'Self-hosted on your own hardware, with scheduled backups that can include the stored files themselves. Nothing phones home, and no licence counts your users.'),
    },
  ]

  return (
    <div style={{ maxWidth: 1080, margin: '0 auto' }}>
      <section className="hlk-about-hero">
        <div className="hlk-about-bar" />
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 18 }}>
          <LogoMark size={40} ink="#F3EFE7" />
          <Wordmark size={30} color="#F3EFE7" />
        </div>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 12, flexWrap: 'wrap', marginBottom: 20 }}>
          <span style={{ fontSize: 22, fontWeight: 700, color: '#F3EFE7', letterSpacing: '.1em' }}>好料庫</span>
          <span className="hlk-mono" style={{ fontSize: 14, color: '#9AA5B5' }}>hó-liāu-khòo</span>
        </div>
        <div style={{ fontSize: 28, fontWeight: 500, color: '#F3EFE7', lineHeight: 1.3, marginBottom: 16 }}>
          &ldquo;the good-stuff store&rdquo;
        </div>
        <p style={{ fontSize: 14, lineHeight: 1.8, color: '#C9CFD8', margin: 0, maxWidth: 620 }}>
          {t('about.tagline', 'A self-hosted home for the packages your team depends on and the builds it produces.')}
        </p>
        {status.data?.version && (
          <div className="hlk-mono" style={{ marginTop: 24, fontSize: 12, color: '#6F7A8A' }}>v{status.data.version}</div>
        )}
      </section>

      <section className="hlk-card" style={{ marginBottom: 24 }}>
        <h2 style={{ fontSize: 18, fontWeight: 600, margin: '0 0 12px' }}>{t('about.nameTitle', 'Where the name comes from')}</h2>
        <p style={{ fontSize: 14, lineHeight: 1.9, color: 'var(--hlk-text-secondary)', margin: '0 0 12px' }}>
          {t('about.nameBody1', 'In Taiwanese Hokkien hó-liāu (好料) means good stuff — good ingredients, good material, the kind worth bringing out for guests. Khòo (庫) is where you keep things. Together: the place the good stuff lives.')}
        </p>
        <p style={{ fontSize: 14, lineHeight: 1.9, color: 'var(--hlk-text-secondary)', margin: '0 0 12px' }}>
          {t('about.nameBody2', 'The packages your builds depend on and the artefacts they produce are exactly that — the good stuff, worth keeping somewhere dependable and close to hand.')}
        </p>
        <p style={{ fontSize: 14, lineHeight: 1.9, color: 'var(--hlk-text-secondary)', margin: 0 }}>
          {t('about.nameBody3', 'The mark is a 广 roof with goods stacked underneath: a storehouse, with the amber line as the light left on.')}
        </p>
      </section>

      <section>
        <h2 style={{ fontSize: 18, fontWeight: 600, margin: '0 0 4px' }}>{t('about.strengthsTitle', 'What it is good at')}</h2>
        <p style={{ fontSize: 13, color: 'var(--hlk-text-tertiary)', margin: '0 0 16px' }}>
          {t('about.strengthsHint', 'Built to replace Nexus without asking anyone to change how they work.')}
        </p>
        <div className="hlk-about-grid">
          {strengths.map((s) => <Strength key={s.title} {...s} />)}
        </div>
      </section>

      <footer style={{ marginTop: 32, paddingTop: 16, borderTop: '1px solid var(--hlk-border)', fontSize: 12, color: 'var(--hlk-text-tertiary)' }}>
        <span style={{ borderLeft: `3px solid ${AMBER}`, paddingLeft: 8 }}>
          {t('about.footer', 'Holiaokho is self-hosted software. It runs on your hardware, stores your artefacts, and answers to nobody else.')}
        </span>
      </footer>
    </div>
  )
}
