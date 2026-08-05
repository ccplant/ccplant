'use client'

import { Suspense, use, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import LoadingSpinner from '../../components/LoadingSpinner'
import AgentAPIChat from '../../components/AgentAPIChat'
import { createAgentAPIProxyClientFromStorage } from '../../../lib/agentapi-proxy-client'
import { waitForSessionMessages, waitForSessionResume } from '../../../lib/session-resume'

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
  const [opened, setOpened] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const client = createAgentAPIProxyClientFromStorage()
    const options = { cancelled: () => cancelled }
    waitForSessionResume(client, sessionId, options)
      .then(() => waitForSessionMessages(client, sessionId, options))
      .then(() => {
        if (!cancelled) setOpened(true)
      })
      .catch((err) => {
        console.error('[SessionPage] Failed to resume session:', err)
        if (!cancelled) setError('セッションを再開できませんでした。')
      })
    return () => { cancelled = true }
  }, [sessionId])

  if (error) {
    return <ChatShell><div className="py-12 text-center text-red-600 dark:text-red-400">{error}</div></ChatShell>
  }

  if (!opened) {
    return <ChatShell><LoadingSpinner /></ChatShell>
  }

  return (
    <ChatShell>
      <Suspense fallback={<LoadingSpinner />}>
        <AgentAPIChat sessionId={sessionId} />
      </Suspense>
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
