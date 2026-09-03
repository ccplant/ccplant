'use client'

import { use } from 'react'
import type { ReactNode } from 'react'
import AgentAPIChat from '../../components/AgentAPIChat'

interface SessionPageProps {
  params: Promise<{
    sessionId: string
  }>
}

export default function SessionPage({ params }: SessionPageProps) {
  // Use React 18's use() hook to unwrap the Promise
  const resolvedParams = use(params)

  return <OpenedSession sessionId={resolvedParams.sessionId} />
}

function OpenedSession({ sessionId }: { sessionId: string }) {
  return (
    <ChatShell>
      <AgentAPIChat sessionId={sessionId} />
    </ChatShell>
  )
}

function ChatShell({ children }: { children: ReactNode }) {
  return (
    <div className="h-dvh bg-gray-50" style={{ position: 'relative', overflow: 'hidden' }}>
      <div className="w-full h-full flex flex-col px-0 sm:px-6 max-w-[1800px] mx-auto" style={{ minHeight: 0 }}>
        {children}
      </div>
    </div>
  )
}
