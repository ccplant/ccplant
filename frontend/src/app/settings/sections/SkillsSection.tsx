'use client'

import { SettingsPageHeader, SkillSettings } from '@/components/settings'
import { useSettingsScope } from '../SettingsScopeContext'

export function SkillsSection() {
  const { settings, update } = useSettingsScope()
  return (
    <>
      <SettingsPageHeader
        title="Skills"
        description="skills.sh packages are installed only for the session's resolved agent type."
      />
      <SkillSettings skills={settings.skills} onChange={(skills) => update({ skills })} />
    </>
  )
}
