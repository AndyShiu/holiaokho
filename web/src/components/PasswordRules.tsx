import { CheckCircleFilled, CloseCircleFilled, MinusCircleOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import type { PasswordPolicy } from '@/api/types'

export function checkPassword(p: PasswordPolicy | undefined, pw: string, username?: string) {
  const rules: { key: string; label: [string, string, Record<string, unknown>?]; ok: boolean | null }[] = []
  if (!p) return rules
  rules.push({ key: 'too_short', label: ['password.rule.minLength', 'At least {{n}} characters', { n: p.minLength }], ok: pw.length >= p.minLength })
  if (p.requireUpper) rules.push({ key: 'need_upper', label: ['password.rule.upper', 'An uppercase letter'], ok: /\p{Lu}/u.test(pw) })
  if (p.requireLower) rules.push({ key: 'need_lower', label: ['password.rule.lower', 'A lowercase letter'], ok: /\p{Ll}/u.test(pw) })
  if (p.requireDigit) rules.push({ key: 'need_digit', label: ['password.rule.digit', 'A digit'], ok: /\p{Nd}/u.test(pw) })
  if (p.requireSymbol) rules.push({ key: 'need_symbol', label: ['password.rule.symbol', 'A symbol'], ok: /[\p{P}\p{S} ]/u.test(pw) })
  if (p.disallowUsername) rules.push({ key: 'contains_username', label: ['password.rule.username', 'Does not contain the username'], ok: !username || username.length < 3 || !pw.toLowerCase().includes(username.toLowerCase()) })
  if (p.disallowCommon) rules.push({ key: 'common', label: ['password.rule.common', 'Not a common password (checked on submit)'], ok: null })
  return rules
}

export function passwordOk(p: PasswordPolicy | undefined, pw: string, username?: string) {
  return checkPassword(p, pw, username).every((r) => r.ok !== false)
}

export function PasswordRules({ policy, value, username, failedCode }: { policy?: PasswordPolicy; value: string; username?: string; failedCode?: string }) {
  const { t } = useTranslation()
  const rules = checkPassword(policy, value, username)
  if (!rules.length) return null
  return (
    <ul style={{ listStyle: 'none', padding: 0, margin: '4px 0 0', fontSize: 12, color: 'var(--hlk-text-secondary)', display: 'grid', gap: 3 }}>
      {rules.map((r) => {
        const failed = failedCode === `password.${r.key}` || (r.ok === false && value.length > 0)
        const icon = failed ? <CloseCircleFilled style={{ color: 'var(--hlk-error)' }} /> : r.ok ? <CheckCircleFilled style={{ color: 'var(--hlk-success)' }} /> : <MinusCircleOutlined style={{ color: 'var(--hlk-text-quaternary)' }} />
        return (
          <li key={r.key} style={{ display: 'flex', gap: 6, alignItems: 'center', color: failed ? 'var(--hlk-error)' : undefined }}>
            {icon}
            {String(t(r.label[0], { defaultValue: r.label[1], ...(r.label[2] ?? {}) }))}
          </li>
        )
      })}
    </ul>
  )
}
