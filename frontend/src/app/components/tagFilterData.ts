import { Session } from '../../types/agentapi'

export interface SidebarTag {
  key: string
  values: string[]
}

export const fallbackTags: SidebarTag[] = [
  { key: 'project_type', values: ['frontend', 'backend', 'fullstack', 'database'] },
  { key: 'technology', values: ['React', 'Node.js', 'PostgreSQL', 'Jest', 'Next.js'] },
  { key: 'priority', values: ['high', 'medium', 'low'] },
]

export function extractSessionTags(sessions: Session[]): SidebarTag[] {
  const tagMap = new Map<string, Set<string>>()
  const add = (key: string, value: unknown) => {
    if (key === 'description' || value === '' || value == null) return
    if (!tagMap.has(key)) tagMap.set(key, new Set())
    tagMap.get(key)?.add(String(value))
  }
  for (const session of sessions) {
    for (const [key, value] of Object.entries(session.tags || {})) add(key, value)
    for (const [key, value] of Object.entries(session.metadata || {})) add(key, value)
  }
  return Array.from(tagMap, ([key, values]) => ({ key, values: Array.from(values).sort() }))
}
