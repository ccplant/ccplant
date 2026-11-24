import type { AgentApiType } from '../../../types/settings'

export type CheckoutTarget = 'branch' | 'pr' | ''

export const getAgentTypeLabel = (agentType: AgentApiType): string => {
  switch (agentType) {
    case 'auto':
      return '自動選択'
    case 'claude-legacy':
      return 'Claude Legacy'
    case 'claude-acp':
      return 'Claude ACP'
    case 'codex-acp':
      return 'Codex ACP'
    case 'pi':
      return 'Pi'
    case 'cursor':
      return 'Cursor ACP'
    case 'antigravity':
      return 'Antigravity'
    default:
      return 'Claude ACP'
  }
}

export function buildRepositoryTags(
  repo: string,
  checkoutTarget: CheckoutTarget,
  branch: string,
  prNumber: string
): Record<string, string> {
  const tags: Record<string, string> = {}
  const trimmedRepo = repo.trim()
  const trimmedBranch = branch.trim()
  const trimmedPrNumber = prNumber.trim()

  if (trimmedRepo) {
    tags.repository = trimmedRepo
  }

  if (trimmedRepo && checkoutTarget === 'branch' && trimmedBranch) {
    tags.branch = trimmedBranch
  }

  if (trimmedRepo && checkoutTarget === 'pr' && trimmedPrNumber) {
    tags.pr_number = trimmedPrNumber
  }

  return tags
}
