'use client'
import type { SandboxPolicySettingsPanelProps } from './SandboxPanelTypes'
import { SandboxPolicySettingsView } from './SandboxPolicySettingsView'
import { useSandboxPolicySettings } from './useSandboxPolicySettings'

export function SandboxPolicySettingsPanel(props: SandboxPolicySettingsPanelProps) {
  return <SandboxPolicySettingsView {...useSandboxPolicySettings(props)} />
}
