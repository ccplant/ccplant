import { AlertTriangle, ArrowRight, Check, ChevronLeft, Loader2, Search, WandSparkles } from 'lucide-react'
import { TeamMigrationState } from './useTeamMigration'
import { MigrationResource, migrationGroups, migrationKey } from './teamMigrationTypes'

export function MigrationProgress({ step }: { step: number }) {
  return <ol className="grid grid-cols-3 gap-2" aria-label="移行ステップ">{['移行元', 'リソース', '完了'].map((label, index) => {
    const number = index + 1; const active = step >= number
    return <li key={label} className={`flex items-center gap-2 border-b-2 px-1 pb-3 text-sm font-medium ${active ? 'border-blue-600 text-blue-700 dark:text-blue-300' : 'border-gray-200 text-gray-400 dark:border-gray-700'}`}><span className={`flex size-6 items-center justify-center rounded-full text-xs ${active ? 'bg-blue-600 text-white' : 'bg-gray-100 dark:bg-gray-800'}`}>{step > number ? <Check className="size-3.5" /> : number}</span>{label}</li>
  })}</ol>
}

export function MigrationSourceStep({ state }: { state: TeamMigrationState }) {
  const disabled = !state.sourceTeam || state.loading || state.sourceMode === 'external' && (!state.apiUrl.trim() || !state.apiToken.trim())
  return <section className="rounded-xl border border-gray-200 bg-white p-6 shadow-sm dark:border-gray-700 dark:bg-gray-900">
    <h2 className="text-base font-semibold text-gray-900 dark:text-white">統合するチームを選択</h2>
    <p className="mt-1 text-sm text-gray-500">選択したチームのリソースを、現在のチームへ移行します。元のチームは削除されません。</p>
    <SourceModeSelector state={state} />
    {state.sourceMode === 'current' ? <CurrentSourceFields state={state} /> : <ExternalSourceFields state={state} />}
    <div className="mt-6 flex items-center justify-end"><button onClick={state.loadResources} disabled={disabled} className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{state.loading && <Loader2 className="size-4 animate-spin" />}リソースを確認<ArrowRight className="size-4" /></button></div>
  </section>
}

function SourceModeSelector({ state }: { state: TeamMigrationState }) {
  return <div className="mt-6 grid grid-cols-2 gap-2 rounded-lg bg-gray-100 p-1 dark:bg-gray-800">{([['current', 'このインスタンス'], ['external', '別のインスタンス']] as const).map(([value, label]) =>
    <button key={value} type="button" onClick={() => { state.setSourceMode(value); state.setSourceTeam(''); state.setError(null) }} className={`rounded-md px-3 py-2 text-sm font-medium ${state.sourceMode === value ? 'bg-white text-gray-900 shadow-sm dark:bg-gray-700 dark:text-white' : 'text-gray-500'}`}>{label}</button>)}</div>
}

function CurrentSourceFields({ state }: { state: TeamMigrationState }) {
  return <><label className="mt-5 block text-sm font-medium text-gray-700 dark:text-gray-200">移行元チーム<select value={state.sourceTeam} onChange={(event) => state.setSourceTeam(event.target.value)} className="mt-2 w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm dark:border-gray-600 dark:bg-gray-950"><option value="">チームを選択してください</option>{state.sourceTeams.map((team) => <option key={team} value={team}>{state.teamName(team)} ({team})</option>)}</select></label>{state.sourceTeams.length === 0 && <p className="mt-3 text-sm text-amber-700 dark:text-amber-300">移行できる別のチームがありません。</p>}</>
}

function ExternalSourceFields({ state }: { state: TeamMigrationState }) {
  return <div className="mt-5 space-y-4">
    <SourceInput label="API URL" value={state.apiUrl} onChange={state.setApiUrl} type="url" placeholder="https://other.example.com/api/v1" />
    <SourceInput label="API token" value={state.apiToken} onChange={state.setApiToken} type="password" placeholder="API token を貼り付け" />
    <SourceInput label="移行元 Team ID" value={state.sourceTeam} onChange={state.setSourceTeam} placeholder="organization/team" />
    <p className="text-xs text-gray-500">token は保存されず、一覧の取得にだけ使用します。移行先には送信されません。</p>
  </div>
}

function SourceInput({ label, value, onChange, type = 'text', placeholder }: { label: string; value: string; onChange: (value: string) => void; type?: string; placeholder: string }) {
  return <label className="block text-sm font-medium text-gray-700 dark:text-gray-200">{label}<input type={type} value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} autoComplete="off" className="mt-2 w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm dark:border-gray-600 dark:bg-gray-950" /></label>
}

export function MigrationResourceStep({ state }: { state: TeamMigrationState }) {
  return <section className="space-y-4">
    <ResourceHeading state={state} />
    {migrationGroups.map((group) => <ResourceGroup key={group.type} state={state} type={group.type} label={group.label} />)}
    <MigrationWarning external={state.sourceMode === 'external'} />
    <div className="flex items-center justify-between"><button onClick={() => state.setStep(1)} disabled={state.migrating} className="inline-flex items-center gap-1 rounded-lg px-3 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"><ChevronLeft className="size-4" />戻る</button><button onClick={state.migrate} disabled={!state.chosen.length || state.migrating} className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">{state.migrating ? <Loader2 className="size-4 animate-spin" /> : <WandSparkles className="size-4" />}{state.migrating ? `${state.completed} / ${state.chosen.length} 移行中` : `${state.chosen.length}件を移行`}</button></div>
  </section>
}

function ResourceHeading({ state }: { state: TeamMigrationState }) {
  return <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold text-gray-900 dark:text-white">移行するリソースを選択</h2><p className="text-sm text-gray-500">{state.sourceMode === 'external' ? state.sourceTeam : state.teamName(state.sourceTeam)} から {state.teamName(state.scopeId)} へ</p></div><div className="relative"><Search className="absolute left-3 top-2.5 size-4 text-gray-400" /><input value={state.query} onChange={(event) => state.setQuery(event.target.value)} placeholder="リソースを検索" className="rounded-lg border border-gray-300 bg-white py-2 pl-9 pr-3 text-sm dark:border-gray-600 dark:bg-gray-900" /></div></div>
}

function ResourceGroup({ state, type, label }: { state: TeamMigrationState; type: MigrationResource['type']; label: string }) {
  const items = state.visible.filter((item) => item.type === type)
  const all = state.resources.filter((item) => item.type === type)
  if (!items.length && state.query) return null
  const allSelected = all.length > 0 && all.every((item) => state.selected.has(migrationKey(item)))
  const toggleAll = () => state.setSelected(toggleResources(state.selected, all, allSelected))
  return <div className="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-900">
    <div className="flex items-center justify-between border-b border-gray-100 bg-gray-50 px-4 py-3 dark:border-gray-800 dark:bg-gray-800/50"><label className="flex items-center gap-3 text-sm font-semibold"><input type="checkbox" checked={allSelected} disabled={!all.length} onChange={toggleAll} />{label}</label><span className="text-xs text-gray-500">{all.filter((item) => state.selected.has(migrationKey(item))).length} / {all.length}</span></div>
    <div className="divide-y divide-gray-100 dark:divide-gray-800">{items.length ? items.map((item) => <ResourceRow key={migrationKey(item)} state={state} item={item} />) : <p className="px-4 py-3 text-sm text-gray-400">リソースはありません</p>}</div>
  </div>
}

function ResourceRow({ state, item }: { state: TeamMigrationState; item: MigrationResource }) {
  const key = migrationKey(item)
  return <label className="flex cursor-pointer items-start gap-3 px-4 py-3 hover:bg-gray-50 dark:hover:bg-gray-800/40"><input className="mt-1" type="checkbox" checked={state.selected.has(key)} onChange={() => state.toggle(key)} /><span className="min-w-0"><span className="block text-sm font-medium text-gray-900 dark:text-white">{item.name}</span>{item.description && <span className="block truncate text-xs text-gray-500">{item.description}</span>}</span></label>
}

function toggleResources(selected: Set<string>, resources: MigrationResource[], remove: boolean) {
  const next = new Set(selected); resources.forEach((item) => remove ? next.delete(migrationKey(item)) : next.add(migrationKey(item))); return next
}

function MigrationWarning({ external }: { external: boolean }) {
  const message = external ? '別インスタンスには元データを残したまま、新しいリソースを作成します。Webhook の secret と SlackBot の token は安全上取得できないため、移行後に再設定してください。' : 'リソースの所有権を付け替えます。Schedule は移行元にも残るコピーとして作成されます。参照するプロファイルも一緒に選択してください。'
  return <div className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><div className="flex gap-2"><AlertTriangle className="mt-0.5 size-4 shrink-0" /><p>{message}</p></div></div>
}

export function MigrationCompleteStep({ state }: { state: TeamMigrationState }) {
  return <section className="rounded-xl border border-emerald-200 bg-emerald-50 p-8 text-center dark:border-emerald-900 dark:bg-emerald-950/20"><span className="mx-auto flex size-12 items-center justify-center rounded-full bg-emerald-600 text-white"><Check className="size-6" /></span><h2 className="mt-4 text-lg font-semibold text-gray-900 dark:text-white">移行が完了しました</h2><p className="mt-1 text-sm text-gray-600 dark:text-gray-300">{state.completed}件のリソースを {state.teamName(state.scopeId)} に{state.sourceMode === 'external' ? '作成' : '付け替え'}しました。</p><button onClick={state.reset} className="mt-6 rounded-lg border border-gray-300 bg-white px-4 py-2 text-sm font-medium hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-900">別のチームを移行</button></section>
}
