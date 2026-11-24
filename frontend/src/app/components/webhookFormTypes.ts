import type { Webhook } from '../../types/webhook'
export interface WebhookFormModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  editingWebhook?: Webhook | null
}

export interface TriggerFormData {
  id?: string
  name: string
  enabled: boolean
  events: string[]
  actions: string[]
  baseBranches: string[]
  initialMessageTemplate: string
  reuseMessageTemplate?: string
  reuseSession: boolean
  mountPayload: boolean
  oneshot: boolean
  sessionTTL: string
  model: string
  credentialSource: '' | 'session_user' | 'triggered_user' | 'team' | 'none'
  goTemplate?: string
  environment: Record<string, string>
  tags: Record<string, string>
  showAdvanced: boolean
  sessionProfileId: string
}

export const emptyTrigger: TriggerFormData = {
  name: '',
  enabled: true,
  events: [],
  actions: [],
  baseBranches: [],
  initialMessageTemplate: '',
  reuseMessageTemplate: '',
  reuseSession: false,
  mountPayload: false,
  oneshot: false,
  sessionTTL: '',
  model: '',
  credentialSource: '',
  goTemplate: '',
  environment: {},
  tags: {},
  showAdvanced: false,
  sessionProfileId: '',
}
