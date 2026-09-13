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

// Catch keys referenced in source but absent from en.json. Covers t('a.b')
// calls and key literals kept in arrays/consts, which a t()-only scan misses.
const srcDir = new URL('../src/', import.meta.url).pathname
const walk = (d) => readdirSync(d, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? walk(join(d, e.name)) : [join(d, e.name)]))
const known = new Set(Object.keys(en))
const prefixes = new Set(Object.keys(en).map((k) => k.split('.')[0]))
const unknown = new Set()
const skip = ['pages/admin/Webhooks.tsx'] // holds backend event names like asset.created
for (const f of walk(srcDir).filter((f) => /\.tsx?$/.test(f) && !skip.some((s) => f.endsWith(s)))) {
  const src = readFileSync(f, 'utf8')
  for (const m of src.matchAll(/['"`]([a-z][a-zA-Z0-9]*(?:\.[a-zA-Z0-9_-]+)+)['"`]/g)) {
    const key = m[1]
    // Only consider strings whose first segment is an existing namespace, and
    // skip dynamic keys (template literals are handled by their prefix).
    if (!prefixes.has(key.split('.')[0])) continue
    if (known.has(key)) continue
    unknown.add(`${f.replace(srcDir, '')}: ${key}`)
  }
}
if (unknown.size) {
  console.error(`keys used in source but missing from en.json:\n  ${[...unknown].join('\n  ')}`)
  bad++
}
// Translations get written by people and machines that do not read every
// script, and a stray Cyrillic or Hangul word inside a Japanese sentence is
// invisible to anyone who cannot read it. Both have shipped here before.
const scripts = {
  cyrillic: /[\u0400-\u04FF]/u,
  hangul: /[\uAC00-\uD7AF\u1100-\u11FF]/u,
  kana: /[\u3040-\u30FF]/u,
}
const forbidden = {
  en: ['cyrillic', 'hangul', 'kana'],
  'zh-TW': ['cyrillic', 'hangul', 'kana'],
  'zh-CN': ['cyrillic', 'hangul', 'kana'],
  ja: ['cyrillic', 'hangul'],
  ko: ['cyrillic', 'kana'],
}
// These deliberately list every language in its own script.
const multiScriptKeys = new Set(['about.s7Body'])
for (const [loc, names] of Object.entries(forbidden)) {
  const flat = locales[loc]
  if (!flat) continue
  for (const [key, value] of Object.entries(flat)) {
    if (typeof value !== 'string') continue
    if (key.startsWith('lang.') || multiScriptKeys.has(key)) continue
    for (const name of names) {
      if (scripts[name].test(value)) {
        console.error(`${loc}.json: ${key} contains ${name} characters — likely a translation slip`)
        bad++
      }
    }
  }
}

console.log(`locales ok: ${Object.keys(locales).join(', ')} (${Object.keys(en).length} keys)`)
process.exit(bad ? 1 : 0)
