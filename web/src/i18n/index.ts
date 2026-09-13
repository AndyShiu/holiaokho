import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import enUS from 'antd/locale/en_US'
import zhTW from 'antd/locale/zh_TW'
import zhCN from 'antd/locale/zh_CN'
import jaJP from 'antd/locale/ja_JP'
import koKR from 'antd/locale/ko_KR'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import localizedFormat from 'dayjs/plugin/localizedFormat'
import 'dayjs/locale/zh-tw'
import 'dayjs/locale/zh-cn'
import 'dayjs/locale/ja'
import 'dayjs/locale/ko'
import en from './locales/en.json'
import zhTWm from './locales/zh-TW.json'
import zhCNm from './locales/zh-CN.json'
import ja from './locales/ja.json'
import ko from './locales/ko.json'

dayjs.extend(relativeTime)
dayjs.extend(localizedFormat)

export const LANGS: { code: string; label: string; short: string }[] = [
  { code: 'en', label: 'English', short: 'EN' },
  { code: 'zh-TW', label: '繁體中文', short: '中文' },
  { code: 'zh-CN', label: '简体中文', short: '中文' },
  { code: 'ja', label: '日本語', short: '日本語' },
  { code: 'ko', label: '한국어', short: '한국어' },
]

function detect(): string {
  try {
    const v = localStorage.getItem('hlk.lang')
    if (v && LANGS.some((l) => l.code === v)) return v
  } catch {}
  const nav = (navigator.language || 'en').toLowerCase()
  if (nav.startsWith('zh')) return nav.includes('cn') || nav.includes('hans') || nav.includes('sg') ? 'zh-CN' : 'zh-TW'
  if (nav.startsWith('ja')) return 'ja'
  if (nav.startsWith('ko')) return 'ko'
  return 'en'
}

i18n.use(initReactI18next).init({
  resources: { en: { translation: en }, 'zh-TW': { translation: zhTWm }, 'zh-CN': { translation: zhCNm }, ja: { translation: ja }, ko: { translation: ko } },
  lng: detect(),
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
  returnEmptyString: false,
})

const dayjsLocale: Record<string, string> = { en: 'en', 'zh-TW': 'zh-tw', 'zh-CN': 'zh-cn', ja: 'ja', ko: 'ko' }
export function setLanguage(code: string) {
  i18n.changeLanguage(code)
  dayjs.locale(dayjsLocale[code] ?? 'en')
  document.documentElement.lang = code
  try {
    localStorage.setItem('hlk.lang', code)
  } catch {}
}
dayjs.locale(dayjsLocale[i18n.language] ?? 'en')
document.documentElement.lang = i18n.language

export function antdLocale(code: string) {
  return { en: enUS, 'zh-TW': zhTW, 'zh-CN': zhCN, ja: jaJP, ko: koKR }[code] ?? enUS
}

export default i18n
