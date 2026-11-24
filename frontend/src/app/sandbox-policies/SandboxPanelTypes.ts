import { SandboxPolicy } from '../../types/sandbox_policy'
export interface SessionDomainImportPanelProps {
  onClose: () => void
  policy: SandboxPolicy
  onImported: () => void
}
export interface SandboxPolicySettingsPanelProps {
  onClose: () => void
  onSuccess: () => void
  editing?: SandboxPolicy | null
}
