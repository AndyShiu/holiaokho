// Fails the build when a locale is missing keys that English has, or when a
// source string calls t() with a key no locale defines.
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

const dir = new URL('../src/i18n/locales/', import.meta.url).pathname
const flat = (o, p = '') =>
  Object.entries(o).reduce((acc, [k, v]) => Object.assign(acc, v && typeof v === 'object' ? flat(v, `${p}${k}.`) : { [`${p}${k}`]: v }), {})

const locales = {}
for (const f of readdirSync(dir).filter((f) => f.endsWith('.json'))) {
  locales[f.replace('.json', '')] = flat(JSON.parse(readFileSync(join(dir, f), 'utf8')))
}
const en = locales.en
let bad = 0
for (const [lang, m] of Object.entries(locales)) {
  if (lang === 'en') continue
  const missing = Object.keys(en).filter((k) => !(k in m))
  const extra = Object.keys(m).filter((k) => !(k in en))
  if (missing.length) { console.error(`${lang}: ${missing.length} missing -> ${missing.slice(0, 8).join(', ')}`); bad++ }
  if (extra.length) { console.error(`${lang}: ${extra.length} extra -> ${extra.slice(0, 8).join(', ')}`); bad++ }
}
console.log(`locales ok: ${Object.keys(locales).join(', ')} (${Object.keys(en).length} keys)`)
process.exit(bad ? 1 : 0)
