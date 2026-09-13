import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { ConfigProvider, App as AntApp } from 'antd'
import { dark, light } from './tokens'
import { antdLocale } from '@/i18n'
import { useTranslation } from 'react-i18next'

type Mode = 'light' | 'dark'
const Ctx = createContext<{ mode: Mode; setMode: (m: Mode) => void }>({ mode: 'light', setMode: () => {} })

function initial(): Mode {
  try {
    const v = localStorage.getItem('hlk.theme')
    if (v === 'dark' || v === 'light') return v
  } catch {}
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<Mode>(initial)
  const { i18n } = useTranslation()
  const setMode = (m: Mode) => {
    setModeState(m)
    try {
      localStorage.setItem('hlk.theme', m)
    } catch {}
  }
  useEffect(() => {
    document.documentElement.dataset.theme = mode
    document.documentElement.style.colorScheme = mode
  }, [mode])
  const value = useMemo(() => ({ mode, setMode }), [mode])
  return (
    <Ctx.Provider value={value}>
      <ConfigProvider theme={mode === 'dark' ? dark : light} locale={antdLocale(i18n.language)}>
        <AntApp>{children}</AntApp>
      </ConfigProvider>
    </Ctx.Provider>
  )
}
export const useTheme = () => useContext(Ctx)
