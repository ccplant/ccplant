'use client'

import { createContext, useContext, useEffect, useMemo, useState } from 'react'

interface SettingsCategoryContextValue {
  activeCategory: string
  setActiveCategory: (category: string) => void
}

const SettingsCategoryContext = createContext<SettingsCategoryContextValue | null>(null)

export function SettingsCategoryProvider({ children }: { children: React.ReactNode }) {
  const [activeCategory, setActiveCategoryState] = useState('settings-overview')

  useEffect(() => {
    const category = window.location.hash.slice(1)
    if (category) setActiveCategoryState(category)
  }, [])

  const value = useMemo(() => ({
    activeCategory,
    setActiveCategory: (category: string) => {
      setActiveCategoryState(category)
      window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}#${category}`)
      window.scrollTo({ top: 0, behavior: 'smooth' })
    },
  }), [activeCategory])

  return <SettingsCategoryContext.Provider value={value}>{children}</SettingsCategoryContext.Provider>
}

export function useSettingsCategory() {
  const context = useContext(SettingsCategoryContext)
  if (!context) throw new Error('useSettingsCategory must be used within SettingsCategoryProvider')
  return context
}
