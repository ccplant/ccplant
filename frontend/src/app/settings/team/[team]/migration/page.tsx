'use client'

import { SettingsPageHeader } from '@/components/settings'
import { MigrationCompleteStep, MigrationProgress, MigrationResourceStep, MigrationSourceStep } from './TeamMigrationSteps'
import { useTeamMigration } from './useTeamMigration'

export default function TeamMigrationPage() {
  const state = useTeamMigration()
  return <div className="max-w-4xl space-y-6">
    <SettingsPageHeader title="移行アシスタント" description={`別のチームから ${state.teamName(state.scopeId)} へリソースを付け替えます。`} />
    <MigrationProgress step={state.step} />
    {state.error && <div role="alert" className="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">{state.error}</div>}
    {state.step === 1 && <MigrationSourceStep state={state} />}
    {state.step === 2 && <MigrationResourceStep state={state} />}
    {state.step === 3 && <MigrationCompleteStep state={state} />}
  </div>
}
