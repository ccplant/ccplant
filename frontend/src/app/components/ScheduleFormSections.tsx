'use client'

import { COMMON_TIMEZONES, type Schedule } from '../../types/schedule'
import CronExpressionInput from './CronExpressionInput'
import KeyValueInput from './KeyValueInput'
import SessionProfileSelect from './SessionProfileSelect'
import type { ScheduleFormModel } from './useScheduleForm'

export function ScheduleTimingFields({ form }: { form: ScheduleFormModel }) {
  return (
    <>
      <div><label htmlFor="name" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">スケジュール名 *</label><input id="name" type="text" value={form.name} onChange={event => form.setName(event.target.value)} placeholder="例: 日次レポート生成" disabled={form.isSubmitting} className="w-full px-3 py-2 border rounded-md dark:bg-gray-700 dark:text-white" required /></div>
      <div><label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">実行タイプ</label><div className="flex space-x-4">
        <label className="flex items-center"><input type="radio" name="executionType" checked={form.executionType === 'recurring'} onChange={() => form.setExecutionType('recurring')} className="mr-2" disabled={form.isSubmitting} />繰り返し</label>
        <label className="flex items-center"><input type="radio" name="executionType" checked={form.executionType === 'onetime'} onChange={() => form.setExecutionType('onetime')} className="mr-2" disabled={form.isSubmitting} />一回限り</label>
      </div></div>
      {form.executionType === 'recurring' ? <div><label className="block text-sm font-medium mb-1">cron式 *</label><CronExpressionInput value={form.cronExpr} onChange={form.setCronExpr} disabled={form.isSubmitting} /></div> : <div><label htmlFor="scheduledAt" className="block text-sm font-medium mb-1">実行日時 *</label><input id="scheduledAt" type="datetime-local" value={form.scheduledAt} onChange={event => form.setScheduledAt(event.target.value)} disabled={form.isSubmitting} className="w-full px-3 py-2 border rounded-md dark:bg-gray-700" required /></div>}
      <div><label htmlFor="timezone" className="block text-sm font-medium mb-1">タイムゾーン</label><select id="timezone" value={form.timezone} onChange={event => form.setTimezone(event.target.value)} disabled={form.isSubmitting} className="w-full px-3 py-2 border rounded-md dark:bg-gray-700">{COMMON_TIMEZONES.map(timezone => <option key={timezone.value} value={timezone.value}>{timezone.label}</option>)}</select></div>
    </>
  )
}

function RepositoryField({ form }: { form: ScheduleFormModel }) {
  return (
    <div className="relative">
      <label htmlFor="repository" className="block text-sm font-medium mb-1">リポジトリ</label>
      <input id="repository" type="text" value={form.repository} onChange={form.handleRepositoryChange} onFocus={form.handleRepositoryFocus} onBlur={form.handleRepositoryBlur} placeholder="例: owner/repository-name" disabled={form.isSubmitting} className="w-full px-3 py-2 border rounded-md dark:bg-gray-700" />
      {form.showRepositorySuggestions && form.repositorySuggestions.length > 0 && <div className="absolute z-10 mt-1 w-full bg-white dark:bg-gray-800 border rounded-md shadow-lg max-h-40 overflow-y-auto">{form.repositorySuggestions.map(suggestion => <button key={suggestion} type="button" onClick={() => form.selectRepositorySuggestion(suggestion)} className="w-full text-left px-3 py-2 text-sm hover:bg-gray-50 dark:hover:bg-gray-700 font-mono">{suggestion}</button>)}</div>}
      <p className="text-xs text-gray-500 mt-1">owner/repository-name の形式で入力してください</p>
    </div>
  )
}

export function ScheduleSessionFields({ form }: { form: ScheduleFormModel }) {
  return (
    <>
      <div><label htmlFor="message" className="block text-sm font-medium mb-1">初期メッセージ</label><textarea id="message" value={form.message} onChange={event => form.setMessage(event.target.value)} placeholder="セッション開始時に送信するメッセージ..." disabled={form.isSubmitting} rows={3} className="w-full px-3 py-2 border rounded-md dark:bg-gray-700 resize-y" /></div>
      <div><label htmlFor="model" className="block text-sm font-medium mb-1">モデル</label><input id="model" type="text" value={form.model} onChange={event => form.setModel(event.target.value)} placeholder="例: gpt-5.4、claude-sonnet-4-6" disabled={form.isSubmitting} className="w-full px-3 py-2 border rounded-md dark:bg-gray-700" /><p className="text-xs text-gray-500 mt-1">空欄の場合はセッションプロファイルまたは既定値を使用します。</p></div>
      <RepositoryField form={form} />
    </>
  )
}

function CheckOption({ id, checked, onChange, label, help, disabled }: { id: string; checked: boolean; onChange: (value: boolean) => void; label: string; help: string; disabled: boolean }) {
  return <div className="flex items-start gap-3"><input type="checkbox" id={id} checked={checked} onChange={event => onChange(event.target.checked)} className="mt-0.5 h-4 w-4 rounded" disabled={disabled} /><div><label htmlFor={id} className="block text-sm font-medium cursor-pointer">{label}</label><p className="text-xs text-gray-500 mt-0.5">{help}</p></div></div>
}

export function ScheduleBehaviorFields({ form }: { form: ScheduleFormModel }) {
  return (
    <>
      <CheckOption id="oneshot" checked={form.oneshot} onChange={form.setOneshot} label="エージェントが終了したら自動的に削除する" help="セッション停止後、自動的に削除します。" disabled={form.isSubmitting} />
      <div className="space-y-3"><CheckOption id="reuseSession" checked={form.reuseSession} onChange={form.setReuseSession} label="実行中のセッションを再利用する" help="アクティブなセッションがあればメッセージを送信します。" disabled={form.isSubmitting} />{form.reuseSession && <div className="ml-7"><label htmlFor="reuseMessage" className="block text-sm font-medium mb-1">再利用時のメッセージ</label><textarea id="reuseMessage" value={form.reuseMessage} onChange={event => form.setReuseMessage(event.target.value)} disabled={form.isSubmitting} rows={2} className="w-full px-3 py-2 border rounded-md dark:bg-gray-700" /></div>}</div>
      <div><label className="block text-sm font-medium mb-1">環境変数</label><KeyValueInput pairs={form.envVarPairs} onChange={form.setEnvVarPairs} disabled={form.isSubmitting} helpText="KEY=VALUE 形式で環境変数を入力してください" /></div>
      <div><label className="block text-sm font-medium mb-1">セッションプロファイル</label><SessionProfileSelect value={form.sessionProfileId} onChange={form.setSessionProfileId} disabled={form.isSubmitting} /></div>
    </>
  )
}

export function CompletedScheduleNotice({ schedule }: { schedule?: Schedule | null }) {
  if (schedule?.status !== 'completed') return null
  return <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md p-3"><p className="text-sm text-blue-700 dark:text-blue-300">完了済みのスケジュールです。更新すると再度アクティブ状態になります。</p></div>
}

export function ScheduleFormActions({ form }: { form: ScheduleFormModel }) {
  return (
    <><>{form.error && <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md p-3"><p className="text-red-600 dark:text-red-400 text-sm">{form.error}</p></div>}</><div className="flex justify-end space-x-3 pt-4"><button type="button" onClick={form.handleClose} disabled={form.isSubmitting} className="px-4 py-2 text-sm bg-gray-100 dark:bg-gray-700 rounded-md disabled:opacity-50">キャンセル</button><button type="submit" disabled={form.isSubmitting} className="px-4 py-2 text-sm text-white bg-blue-600 hover:bg-blue-700 rounded-md disabled:bg-gray-400">{form.isSubmitting ? '保存中...' : form.isEditing ? '更新' : '作成'}</button></div></>
  )
}
