'use client'

import { ConversationListView } from './ConversationListView'
import { useConversationList } from './useConversationList'

export default function ConversationList() {
  const model = useConversationList()
  return <ConversationListView model={model} />
}
