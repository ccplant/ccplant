'use client'

import { use, useEffect } from 'react'
import type { ReactNode } from 'react'
import AgentAPIChat from '../../components/AgentAPIChat'
import { createAgentAPIProxyClientFromStorage } from '../../../lib/agentapi-proxy-client'
import { waitForSessionResume } from '../../../lib/session-resume'

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
  useEffect(() => {
    let cancelled = false
    const client = createAgentAPIProxyClientFromStorage()
    const options = { cancelled: () => cancelled }

    // Opening an active session immediately starts the bridge probe and history
    // request in AgentAPIChat. Resume is only useful for suspended workloads, so
    // keep it off that critical path (and away from constrained session tunnels).
    const resumeTimer = setTimeout(() => {
      waitForSessionResume(client, sessionId, options)
        .catch((err) => {
          if (!cancelled) console.error('[SessionPage] Failed to resume session:', err)
        })
    }, 3000)

    return () => {
      cancelled = true
      clearTimeout(resumeTimer)
    }
  }, [sessionId])

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
