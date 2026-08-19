'use client'

import { createContext, useContext, useEffect, useMemo, useState } from 'react'

interface SettingsCategoryContextValue {
  activeCategory: string
  setActiveCategory: (category: string) => void
}

const SettingsCategoryContext = createContext<SettingsCategoryContextValue>({
  activeCategory: '*',
  setActiveCategory: () => undefined,
})

export function SettingsCategoryProvider({ children }: { children: React.ReactNode }) {
  const [activeCategory, setActiveCategoryState] = useState('ai-authentication')

  useEffect(() => {
    const category = window.location.hash.slice(1)
    const validCategories = ['ai-authentication', 'extensions', 'session-settings', 'client-settings', 'notification-settings', 'security-settings']
    if (validCategories.includes(category)) setActiveCategoryState(category)
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
  return useContext(SettingsCategoryContext)
}
