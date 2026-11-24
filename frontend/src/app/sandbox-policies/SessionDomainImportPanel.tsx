'use client'
import type { SessionDomainImportPanelProps } from './SandboxPanelTypes'
import { SessionDomainImportView } from './SessionDomainImportView'
import { useSessionDomainImport } from './useSessionDomainImport'

export function SessionDomainImportPanel(props: SessionDomainImportPanelProps) {
  return <SessionDomainImportView {...useSessionDomainImport(props)} />
}
