import type { AgentApiType } from '../../../types/settings'
import type { CheckoutTarget } from './newSessionPresentation'

export interface DockerRegistryDraft {
  server: string
  username: string
  password: string
  secretName: string
  insecure: boolean
}

export interface NewSessionDraft {
  initialMessage: string
  freeFormRepository: string
  checkoutTarget: CheckoutTarget
  checkoutBranch: string
  checkoutPrNumber: string
  selectedTeam: string | null
  selectedAgentType: AgentApiType
  selectedManagerId: string
  connectionId: string
  cycleEnabled: boolean
  cycleMessage: string
  cycleMaxCount: number
  dockerEnabled: boolean
  dockerRegistries: DockerRegistryDraft[]
  sessionTTL: string
  sessionProfileId: string
}
