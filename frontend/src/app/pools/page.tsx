'use client'

import { Suspense, useEffect } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import { useTeamScope } from '@/contexts/TeamScopeContext'
import TopBar from '../components/TopBar'
import { SaveBar } from '../settings/SaveBar'
import { SettingsScopeProvider } from '../settings/SettingsScopeContext'
import { PoolsSection } from '../settings/sections/PoolsSection'
import { ScopeGate } from '../settings/sections/ScopeGate'

function PoolsWorkspace() {
  return (
    <ScopeGate>
      <PoolsSection showHeader={false} />
      <SaveBar />
    </ScopeGate>
  )
}

function PoolsPageContent() {
  const searchParams = useSearchParams()
  const router = useRouter()
  const requestedTeam = searchParams.get('team')
  const { selectedTeam, availableTeams, selectTeam, isLoading } = useTeamScope()

  useEffect(() => {
    if (!requestedTeam || isLoading) return
    if (availableTeams.length === 0 || availableTeams.includes(requestedTeam)) {
      selectTeam(requestedTeam)
      router.replace('/pools')
    }
  }, [requestedTeam, availableTeams, isLoading, router, selectTeam])

  const team = requestedTeam || selectedTeam
  const scopeKind = team ? 'team' : 'personal'

  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-950">
      <TopBar
        title=""
      >
      </TopBar>
      <div className="flex">
        <div className="min-w-0 flex-1 px-4 py-6 md:px-6 md:py-8 lg:px-8">
          <SettingsScopeProvider
            key={`${scopeKind}:${team ?? ''}`}
            scopeKind={scopeKind}
            teamId={team ?? undefined}
          >
            <PoolsWorkspace />
          </SettingsScopeProvider>
        </div>
      </div>
    </main>
  )
}

export default function PoolsPage() {
  return (
    <Suspense fallback={<div className="min-h-dvh bg-gray-50 dark:bg-gray-950" />}>
      <PoolsPageContent />
    </Suspense>
  )
}
