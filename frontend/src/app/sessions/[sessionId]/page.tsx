'use client'

import { Suspense, use, useEffect, useState } from 'react'
import LoadingSpinner from '../../components/LoadingSpinner'
import AgentAPIChat from '../../components/AgentAPIChat'
import { createAgentAPIProxyClientFromStorage } from '../../../lib/agentapi-proxy-client'

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
    client.resumeSession(sessionId)
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
    return <div className="h-dvh flex items-center justify-center text-red-600 dark:text-red-400">{error}</div>
  }

  if (!opened) return <LoadingSpinner />

  return (
    <div className="h-dvh bg-gray-50" style={{ position: 'relative', overflow: 'hidden' }}>
      <div className="w-full h-full flex flex-col px-0 sm:px-6 max-w-[1800px] mx-auto" style={{ minHeight: 0 }}>
        <Suspense fallback={<LoadingSpinner />}>
          <AgentAPIChat sessionId={sessionId} />
        </Suspense>
      </div>
    </div>
  )
}
