import type { Dispatch, SetStateAction } from 'react'
import { GITHUB_ACTIONS, type WebhookType } from '../../types/webhook'
import { emptyTrigger, type TriggerFormData } from './webhookFormTypes'

type Context = {
  triggers: TriggerFormData[]
  expandedTrigger: number | null
  webhookType: WebhookType
  setTriggers: Dispatch<SetStateAction<TriggerFormData[]>>
  setExpandedTrigger: Dispatch<SetStateAction<number | null>>
}

function createTriggerMutations({ triggers, expandedTrigger, webhookType, setTriggers, setExpandedTrigger }: Context) {
  const addTrigger = () => {
    setTriggers((prev) => [...prev, { ...emptyTrigger }])
    setExpandedTrigger(triggers.length)
  }

  const removeTrigger = (index: number) => {
    if (triggers.length <= 1) return
    setTriggers((prev) => prev.filter((_, i) => i !== index))
    if (expandedTrigger === index) {
      setExpandedTrigger(null)
    } else if (expandedTrigger !== null && expandedTrigger > index) {
      setExpandedTrigger(expandedTrigger - 1)
    }
  }

  const updateTrigger = (index: number, field: keyof TriggerFormData, value: unknown) => {
    setTriggers((prev) => prev.map((t, i) => (i === index ? { ...t, [field]: value } : t)))
  }

  const updateCredentialSource = (index: number, credentialSource: TriggerFormData['credentialSource']) => {
    setTriggers((prev) =>
      prev.map((trigger, i) => {
        if (i !== index) return trigger
        if (credentialSource !== 'triggered_user' || trigger.tags.username) {
          return { ...trigger, credentialSource }
        }
        return {
          ...trigger,
          credentialSource,
          tags: {
            ...trigger.tags,
            username: webhookType === 'github' ? '{{.sender.login}}' : '{{.user.login}}',
          },
        }
      })
    )
  }

  return { addTrigger, removeTrigger, updateTrigger, updateCredentialSource }
}

function createTriggerSelections(
  triggers: TriggerFormData[],
  updateTrigger: ReturnType<typeof createTriggerMutations>['updateTrigger']
) {
  const toggleTriggerEvent = (index: number, event: string) => {
    const trigger = triggers[index]
    const newEvents = trigger.events.includes(event)
      ? trigger.events.filter((e) => e !== event)
      : [...trigger.events, event]
    updateTrigger(index, 'events', newEvents)
  }

  const toggleTriggerAction = (index: number, action: string) => {
    const trigger = triggers[index]
    const newActions = trigger.actions.includes(action)
      ? trigger.actions.filter((a) => a !== action)
      : [...trigger.actions, action]
    updateTrigger(index, 'actions', newActions)
  }

  const getAvailableActions = (events: string[]): { label: string; value: string }[] => {
    const allActions: { label: string; value: string }[] = []
    const seen = new Set<string>()

    for (const event of events) {
      const actions = GITHUB_ACTIONS[event as keyof typeof GITHUB_ACTIONS]
      if (actions) {
        for (const action of actions) {
          if (!seen.has(action.value)) {
            seen.add(action.value)
            allActions.push(action)
          }
        }
      }
    }

    return allActions
  }

  return { toggleTriggerEvent, toggleTriggerAction, getAvailableActions }
}

function createTriggerMetadataActions(
  triggers: TriggerFormData[],
  updateTrigger: ReturnType<typeof createTriggerMutations>['updateTrigger']
) {
  // Helper function to add key-value pair
  const addKeyValuePair = (index: number, field: 'environment' | 'tags') => {
    const trigger = triggers[index]
    const newKey = `key_${Object.keys(trigger[field]).length + 1}`
    updateTrigger(index, field, { ...trigger[field], [newKey]: '' })
  }

  // Helper function to update key-value pair
  const updateKeyValuePair = (
    index: number,
    field: 'environment' | 'tags',
    oldKey: string,
    newKey: string,
    newValue: string
  ) => {
    const trigger = triggers[index]
    const updated = Object.fromEntries(
      Object.entries(trigger[field]).map(([key, value]) => (key === oldKey ? [newKey, newValue] : [key, value]))
    )

    updateTrigger(index, field, updated)
  }

  // Helper function to remove key-value pair
  const removeKeyValuePair = (index: number, field: 'environment' | 'tags', key: string) => {
    const trigger = triggers[index]
    const updated = { ...trigger[field] }
    delete updated[key]
    updateTrigger(index, field, updated)
  }

  return { addKeyValuePair, updateKeyValuePair, removeKeyValuePair }
}

export function createWebhookTriggerActions(context: Context) {
  const mutations = createTriggerMutations(context)
  return {
    ...mutations,
    ...createTriggerSelections(context.triggers, mutations.updateTrigger),
    ...createTriggerMetadataActions(context.triggers, mutations.updateTrigger),
  }
}
