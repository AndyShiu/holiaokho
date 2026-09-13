import { useTranslation } from 'react-i18next'
import { useTheme } from '@/theme/ThemeContext'
import { typeTag } from '@/theme/tokens'

export function TypeTag({ type, small }: { type: 'hosted' | 'proxy' | 'group' | 'offline' | 'blocked'; small?: boolean }) {
  const { t } = useTranslation()
  const { mode } = useTheme()
  const c = typeTag[mode][type]
  const label = { hosted: t('type.hosted', 'Hosted'), proxy: t('type.proxy', 'Proxy'), group: t('type.group', 'Group'), offline: t('type.offline', 'Offline'), blocked: t('type.blocked', 'Blocked') }[type]
  return (
    <span style={{ display: 'inline-block', padding: small ? '0 5px' : '1px 7px', borderRadius: 4, fontSize: small ? 10 : 11, fontWeight: 500, background: c.bg, color: c.fg, border: `1px solid ${c.border}`, lineHeight: '17px', whiteSpace: 'nowrap' }}>
      {label}
    </span>
  )
}
