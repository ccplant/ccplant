'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type DefaultModelTarget =
  | 'codex:all'
  | 'codex:auth_json'
  | 'codex:openai_compatible'
  | 'claude:all'
  | 'claude:oauth'
  | 'claude:bedrock'
  | 'claude:anthropic_compatible'

const DEFAULT_MODEL_TARGETS = [
  { value: 'codex:all', label: 'Codex / 全認証方式' },
  { value: 'codex:auth_json', label: 'Codex / auth.json' },
  { value: 'codex:openai_compatible', label: 'Codex / OpenAI 互換 API' },
  { value: 'claude:all', label: 'Claude / 全認証方式' },
  { value: 'claude:oauth', label: 'Claude / OAuth' },
  { value: 'claude:bedrock', label: 'Claude / Bedrock' },
  { value: 'claude:anthropic_compatible', label: 'Claude / Anthropic 互換 API' },
] as const satisfies ReadonlyArray<{ value: DefaultModelTarget; label: string }>

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  | 'codexModel'
  | 'setCodexModel'
  | 'claudeModel'
  | 'setClaudeModel'
  | 'codexDefaultModels'
  | 'setCodexDefaultModels'
  | 'claudeDefaultModels'
  | 'setClaudeDefaultModels'
  | 'defaultModelTarget'
  | 'setDefaultModelTarget'
  | 'modelOptions'
  | 'setModelOptions'
  | 'setDirty'
> &
  Pick<SessionProfileEditorProps, never>

export function ProfileModelsSection({
  codexModel,
  setCodexModel,
  claudeModel,
  setClaudeModel,
  codexDefaultModels,
  setCodexDefaultModels,
  claudeDefaultModels,
  setClaudeDefaultModels,
  defaultModelTarget,
  setDefaultModelTarget,
  modelOptions,
  setModelOptions,
  setDirty,
}: Props) {
  return (
    <div className="space-y-5">
      <div className="space-y-3">
        <p className="text-sm font-medium">エージェントごとのモデル</p>
        <p className="text-xs text-gray-500">
          認証方式別の値が最優先です。「全認証方式」は個別の値がない場合のフォールバックとして使われます。
        </p>
        <label className="block text-sm">
          認証方式
          <select
            aria-label="デフォルトモデルの認証方式"
            value={defaultModelTarget}
            onChange={(e) => setDefaultModelTarget(e.target.value as DefaultModelTarget)}
            className="w-full rounded-md border bg-white p-2 dark:bg-gray-800"
          >
            {DEFAULT_MODEL_TARGETS.map(({ value, label }) => {
              const [agent, mode] = value.split(':')
              const configured =
                mode === 'all'
                  ? Boolean((agent === 'codex' ? codexModel : claudeModel).trim())
                  : agent === 'codex'
                    ? Boolean(codexDefaultModels[mode as keyof typeof codexDefaultModels]?.trim())
                    : Boolean(claudeDefaultModels[mode as keyof typeof claudeDefaultModels]?.trim())
              return (
                <option key={value} value={value}>
                  {label}
                  {configured ? ' （設定済み）' : ''}
                </option>
              )
            })}
          </select>
        </label>
        <label className="block text-sm">
          デフォルトモデル
          <input
            aria-label="認証方式別のデフォルトモデル"
            value={
              defaultModelTarget.startsWith('codex:')
                ? defaultModelTarget === 'codex:all'
                  ? codexModel
                  : (codexDefaultModels[defaultModelTarget.slice(6) as keyof typeof codexDefaultModels] ?? '')
                : defaultModelTarget === 'claude:all'
                  ? claudeModel
                  : (claudeDefaultModels[defaultModelTarget.slice(7) as keyof typeof claudeDefaultModels] ?? '')
            }
            onChange={(e) => {
              if (defaultModelTarget === 'codex:all') {
                setCodexModel(e.target.value)
              } else if (defaultModelTarget === 'claude:all') {
                setClaudeModel(e.target.value)
              } else if (defaultModelTarget.startsWith('codex:')) {
                setCodexDefaultModels((current) => ({ ...current, [defaultModelTarget.slice(6)]: e.target.value }))
              } else {
                setClaudeDefaultModels((current) => ({ ...current, [defaultModelTarget.slice(7)]: e.target.value }))
              }
              setDirty(true)
            }}
            placeholder="接続設定から継承"
            className="w-full rounded-md border bg-white p-2 dark:bg-gray-800"
          />
        </label>
        <label className="block text-sm">
          モデル候補（ACP チャットの切り替え候補）
          <textarea
            aria-label="モデル候補"
            value={modelOptions}
            onChange={(e) => {
              setModelOptions(e.target.value)
              setDirty(true)
            }}
            placeholder={'1 行に 1 つ入力\nsonnet\nopus'}
            rows={3}
            className="w-full rounded-md border bg-white p-2 font-mono text-xs dark:bg-gray-800"
          />
        </label>
        <p className="text-xs text-gray-500">
          セッション情報パネルの「Switch
          Model」に表示する候補です（エージェントが提示する候補も常に表示されます）。codex-acp ではここで指定した候補が
          Codex のモデルカタログに登録され、セッション開始後に選択できます。登録に失敗した場合や Claude
          では、エージェントが候補を拒否することがあるため、切り替えに失敗するときは既定モデル（Codex / Claude モデル
          ID）を変えて新しいセッションを開始してください。
        </p>
      </div>
    </div>
  )
}
