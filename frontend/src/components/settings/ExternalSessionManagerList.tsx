'use client'

import { ExternalSessionManagerConfig } from '@/types/settings'
import { ItemList, ItemListEmpty } from './ui/ItemList'
import { ExternalSessionManagerRow } from './ExternalSessionManagerRow'
import { useExternalSessionManagerList } from './useExternalSessionManagerList'

interface Props { managers: ExternalSessionManagerConfig[]; onChange: (managers: ExternalSessionManagerConfig[]) => void; revealedTokens: Record<string, string>; onRegenerate: (esmId: string) => void; regeneratingEsmId: string | null; scope: 'user' | 'team'; teamId?: string }

export function ExternalSessionManagerList({ managers, onChange, revealedTokens, onRegenerate, regeneratingEsmId, scope, teamId }: Props) {
  const state = useExternalSessionManagerList({ managers, onChange, scope, teamId })
  if (!managers.length) return <ItemList><ItemListEmpty>External Session Manager は登録されていません</ItemListEmpty></ItemList>
  return <ItemList>{managers.map((manager, index) => <ExternalSessionManagerRow key={manager.id || index} manager={manager} index={index} token={manager.id ? revealedTokens[manager.id] : undefined} state={state} onRegenerate={onRegenerate} regeneratingEsmId={regeneratingEsmId} />)}</ItemList>
}
