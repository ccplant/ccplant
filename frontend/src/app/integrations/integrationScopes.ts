import { IntegrationScope, SciaIntegration } from '@/types/settings'

export const denyScopeValue = '__none__'

export function defaultScopeGroups(integration: SciaIntegration): Record<string, string> {
  const selected: Record<string, string> = {}
  for (const group of scopeGroups(integration)) {
    selected[group.id] = defaultScopeForGroup(group)
  }
  return selected
}

export function defaultScopeForGroup(group: ScopeGroup): string {
  return group.scopes.find((scope) => scope.enabled)?.id || denyScopeValue
}

export function fallbackScopeForGroup(group: ScopeGroup): string {
  return group.scopes.find((scope) => scope.enabled)?.id || group.scopes[0]?.id || denyScopeValue
}

export function scopeIDsForSelectedGroups(integration: SciaIntegration, selectedGroups: Record<string, string>): string[] {
  const scopeIDs = new Set(integration.scopes.map((scope) => scope.id))
  return Object.values(selectedGroups).filter((scopeID) => scopeID !== denyScopeValue && scopeIDs.has(scopeID))
}

export type ScopeGroup = { id: string; name: string; desc?: string; scopes: IntegrationScope[] }

export function scopeGroups(integration: SciaIntegration): ScopeGroup[] {
  const groups = new Map<string, ScopeGroup>()
  for (const scope of integration.scopes) {
    const groupID = scope.group || scope.id
    const group = groups.get(groupID) || {
      id: groupID,
      name: scope.group_name || scope.name,
      desc: scope.group_desc,
      scopes: [],
    }
    group.scopes.push(scope)
    groups.set(groupID, group)
  }
  return Array.from(groups.values())
}
