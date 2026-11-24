'use client'

import type { KeyboardEvent } from 'react'
import SessionProfileSelect from './SessionProfileSelect'
import type { KeyValuePair, SlackbotFormModel } from './useSlackbotForm'

export function SlackbotFormHeader({ isEditing, onClose }: { isEditing: boolean; onClose: () => void }) {
  return (
    <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
      <div className="flex items-center gap-2">
        <div className="w-7 h-7 rounded-lg bg-purple-100 dark:bg-purple-900/30 flex items-center justify-center">
          <svg className="w-4 h-4 text-purple-600 dark:text-purple-400" fill="currentColor" viewBox="0 0 24 24"><path d="M6 15a2 2 0 0 1-2 2 2 2 0 0 1-2-2 2 2 0 0 1 2-2h2v2zm1 0a2 2 0 0 1 2-2 2 2 0 0 1 2 2v5a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-5zm2-8a2 2 0 0 1-2-2 2 2 0 0 1 2-2 2 2 0 0 1 2 2v2H9zm0 1a2 2 0 0 1 2 2 2 2 0 0 1-2 2H4a2 2 0 0 1-2-2 2 2 0 0 1 2-2h5zm8 2a2 2 0 0 1 2-2 2 2 0 0 1 2 2 2 2 0 0 1-2 2h-2v-2zm-1 0a2 2 0 0 1-2 2 2 2 0 0 1-2-2V5a2 2 0 0 1 2-2 2 2 0 0 1 2 2v5zm-2 8a2 2 0 0 1 2 2 2 2 0 0 1-2 2 2 2 0 0 1-2-2v-2h2zm0-1a2 2 0 0 1-2-2 2 2 0 0 1 2-2h5a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-5z" /></svg>
        </div>
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white">{isEditing ? 'Slackbot を編集' : '新しいSlackbot'}</h2>
      </div>
      <button onClick={onClose} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors" aria-label="Close"><svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" /></svg></button>
    </div>
  )
}

interface ChipFieldProps {
  label: string; help: string; placeholder: string; values: string[]; input: string; required?: boolean; mono?: boolean
  setInput: (value: string) => void; add: () => void; remove: (value: string) => void; onKeyDown: (event: KeyboardEvent<HTMLInputElement>) => void
}

function ChipField(props: ChipFieldProps) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{props.label}{props.required && <span className="text-red-500"> *</span>}</label>
      <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">{props.help}</p>
      <div className="flex flex-wrap gap-1.5 mb-2">
        {props.values.map(value => <span key={value} className={`inline-flex items-center px-2 py-0.5 rounded text-xs bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 ${props.mono ? 'font-mono' : ''}`}>{value}<button type="button" onClick={() => props.remove(value)} className="ml-1">×</button></span>)}
      </div>
      <div className="flex gap-2">
        <input type="text" value={props.input} onChange={event => props.setInput(event.target.value)} onKeyDown={props.onKeyDown} onBlur={props.add} placeholder={props.placeholder} className={`flex-1 px-3 py-2 border rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 ${props.mono ? 'font-mono' : ''} ${props.required && !props.values.length ? 'border-red-400' : 'border-gray-300 dark:border-gray-600'}`} />
        <button type="button" onClick={props.add} className="px-3 py-2 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-lg">追加</button>
      </div>
    </div>
  )
}

export function SlackbotBasicFields({ model }: { model: SlackbotFormModel }) {
  return (
    <>
      {model.error && <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg"><p className="text-sm text-red-700 dark:text-red-400">{model.error}</p></div>}
      <div><label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">名前 <span className="text-red-500">*</span></label><input type="text" value={model.name} onChange={event => model.setName(event.target.value)} placeholder="例: my-slackbot" className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500" required /></div>
      <ChipField label="チャンネル名フィルタ" help="処理するチャンネル名を1つ以上指定してください。Enter またはカンマで追加" placeholder="例: dev-alerts, backend" values={model.allowedChannelNames} input={model.channelNameInput} setInput={model.setChannelNameInput} add={model.addChannelName} remove={model.removeChannelName} onKeyDown={model.handleChannelNameKeyDown} required />
      <ChipField label="発話ユーザー制限" help="許可する Slack ユーザー ID。空の場合は全ユーザーを許可します。" placeholder="例: U012AB3CD" values={model.allowedUserIDs} input={model.userIDInput} setInput={model.setUserIDInput} add={model.addUserID} remove={model.removeUserID} onKeyDown={model.handleUserIDKeyDown} mono />
    </>
  )
}

function TokenField({ label, value, setValue, isEditing, placeholder }: { label: string; value: string; setValue: (value: string) => void; isEditing: boolean; placeholder: string }) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{label}</label>
      {isEditing && <p className="text-xs text-yellow-600 dark:text-yellow-400 mb-1">変更する場合のみ入力してください。空欄の場合は現在の値が保持されます。</p>}
      <input type="password" value={value} onChange={event => setValue(event.target.value)} placeholder={isEditing ? `変更する場合のみ入力 (${placeholder})` : placeholder} autoComplete="new-password" className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono" />
    </div>
  )
}

export function SlackAppSection({ model }: { model: SlackbotFormModel }) {
  return (
    <div>
      <label className="flex items-start gap-2 cursor-pointer"><input type="checkbox" checked={model.showBotTokenSection} onChange={event => model.setShowBotTokenSection(event.target.checked)} className="mt-0.5 h-4 w-4 rounded" /><div><span className="block text-sm font-medium text-gray-700 dark:text-gray-300">カスタム Slack App を使用</span><span className="block text-xs text-gray-500 dark:text-gray-400 mt-0.5">Bot Token と App-Level Token を個別に設定します</span></div></label>
      {model.showBotTokenSection && <div className="mt-4 space-y-4 pl-4 border-l-2 border-blue-200 dark:border-blue-700">
        {model.availableSecrets.length > 0 && <div><label className="block text-sm font-medium mb-1">保存済み Slack App</label><select value={model.selectedSecretId} onChange={event => model.setSelectedSecretId(event.target.value)} className="w-full px-3 py-2 border rounded-lg bg-white dark:bg-gray-700"><option value="">新しい Slack App を設定</option>{model.availableSecrets.map(secret => <option key={secret.id} value={secret.id}>{secret.name}</option>)}</select></div>}
        {!model.selectedSecretId && <><TokenField label="Bot Token" value={model.botToken} setValue={model.setBotToken} isEditing={model.isEditing} placeholder="xoxb-..." /><TokenField label="App-Level Token" value={model.appToken} setValue={model.setAppToken} isEditing={model.isEditing} placeholder="xapp-..." /></>}
      </div>}
    </div>
  )
}

function TextSetting({ label, value, setValue, help }: { label: string; value: string; setValue: (value: string) => void; help: string }) {
  return <div><label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{label}</label><textarea value={value} onChange={event => setValue(event.target.value)} rows={2} className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 font-mono resize-y" /><p className="text-xs text-gray-500 dark:text-gray-400 mt-1">{help}</p></div>
}

function ToggleSetting({ label, help, value, setValue }: { label: string; help: string; value: boolean; setValue: (value: boolean) => void }) {
  return <div className="flex items-center justify-between"><div><label className="block text-sm font-medium text-gray-700 dark:text-gray-300">{label}</label><p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{help}</p></div><button type="button" role="switch" aria-checked={value} onClick={() => setValue(!value)} className={`relative inline-flex h-6 w-11 items-center rounded-full ${value ? 'bg-blue-600' : 'bg-gray-300 dark:bg-gray-600'}`}><span className={`inline-block h-4 w-4 transform rounded-full bg-white shadow ${value ? 'translate-x-6' : 'translate-x-1'}`} /></button></div>
}

interface PairEditorProps {
  label: string; pairs: KeyValuePair[]; setPairs: SlackbotFormModel['setEnvPairs']; model: SlackbotFormModel
}

function PairEditor({ label, pairs, setPairs, model }: PairEditorProps) {
  return (
    <div><label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{label}</label><div className="space-y-2">
      {pairs.map((pair, index) => <div key={index} className="flex items-center gap-2"><input type="text" value={pair.key} onChange={event => model.updatePair(pairs, setPairs, index, 'key', event.target.value)} placeholder="KEY" className="flex-1 px-2 py-1.5 border rounded text-xs bg-white dark:bg-gray-700 font-mono" /><span>=</span><input type="text" value={pair.value} onChange={event => model.updatePair(pairs, setPairs, index, 'value', event.target.value)} placeholder="value" className="flex-1 px-2 py-1.5 border rounded text-xs bg-white dark:bg-gray-700 font-mono" /><button type="button" onClick={() => model.removePair(pairs, setPairs, index)} aria-label={`Remove ${label}`}>×</button></div>)}
      <button type="button" onClick={() => model.addPair(setPairs)} className="text-xs text-blue-600 dark:text-blue-400">＋ 追加</button>
    </div></div>
  )
}

function AdvancedContent({ model }: { model: SlackbotFormModel }) {
  return (
    <div className="mt-4 space-y-5 pl-4 border-l-2 border-gray-200 dark:border-gray-700">
      <TextSetting label="初期メッセージテンプレート" value={model.initialMessageTemplate} setValue={model.setInitialMessageTemplate} help="新規セッション作成時に送信されるGoテンプレート" />
      <TextSetting label="再利用メッセージテンプレート" value={model.reuseMessageTemplate} setValue={model.setReuseMessageTemplate} help="既存セッション再利用時に送信されるGoテンプレート" />
      <div><label className="block text-sm font-medium mb-1">初期リポジトリ</label><input type="text" value={model.repoFullName} onChange={event => model.setRepoFullName(event.target.value)} placeholder="例: myorg/myrepo" className="w-full px-3 py-2 border rounded-lg bg-white dark:bg-gray-700 font-mono" /></div>
      <ToggleSetting label="セッション作成通知" help="セッション作成時に Slack へ URL 通知を投稿する" value={model.notifyOnSessionCreated} setValue={model.setNotifyOnSessionCreated} />
      <ToggleSetting label="ボットからのメンションに反応する" help="他のボットからのメンションにも反応する" value={model.allowBotMessages} setValue={model.setAllowBotMessages} />
      <div><label className="block text-sm font-medium mb-1">最大セッション数</label><input type="number" min={1} max={100} value={model.maxSessions} onChange={event => model.setMaxSessions(Number(event.target.value))} className="w-32 px-3 py-2 border rounded-lg" /></div>
      <PairEditor label="環境変数" pairs={model.envPairs} setPairs={model.setEnvPairs} model={model} />
      <div><label className="block text-sm font-medium mb-1">セッションプロファイル</label><SessionProfileSelect value={model.sessionProfileId} onChange={model.setSessionProfileId} disabled={model.isSubmitting} /></div>
      <PairEditor label="タグ" pairs={model.tagPairs} setPairs={model.setTagPairs} model={model} />
    </div>
  )
}

export function SlackbotAdvancedSection({ model }: { model: SlackbotFormModel }) {
  return <div><button type="button" onClick={() => model.setShowAdvanced(!model.showAdvanced)} className="flex items-center gap-1 text-sm text-gray-600 dark:text-gray-400"><span className={model.showAdvanced ? 'rotate-90' : ''}>›</span>詳細設定</button>{model.showAdvanced && <AdvancedContent model={model} />}</div>
}

export function SlackbotFormFooter({ model, onClose }: { model: SlackbotFormModel; onClose: () => void }) {
  return <div className="flex items-center justify-end gap-3 px-6 py-4 border-t border-gray-200 dark:border-gray-700"><button type="button" onClick={onClose} disabled={model.isSubmitting} className="px-4 py-2 text-sm border rounded-lg disabled:opacity-50">キャンセル</button><button type="submit" onClick={model.handleSubmit} disabled={model.isSubmitting} className="px-4 py-2 text-sm text-white bg-blue-600 hover:bg-blue-700 rounded-lg disabled:opacity-50">{model.isSubmitting ? '保存中…' : model.isEditing ? '保存' : '作成'}</button></div>
}
