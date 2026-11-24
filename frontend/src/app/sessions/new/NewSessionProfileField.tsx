'use client'
import SessionProfileSelect from '../../components/SessionProfileSelect'
import type { NewSessionFormState } from './useNewSession'

export function NewSessionProfileField(context: NewSessionFormState) {
  const { previewTags, isCreating, sessionProfileId, setSessionProfileId } = context
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">セッションプロファイル</label>
      <SessionProfileSelect
        value={sessionProfileId}
        onChange={setSessionProfileId}
        disabled={isCreating}
        previewTags={Object.keys(previewTags).length > 0 ? previewTags : undefined}
      />
      <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">
        プロファイルを選択すると、環境変数・タグ・テンプレートなどの設定を適用します。「自動選択」の場合はリポジトリの
        selector_tags に一致するプロファイルが優先され、該当がなければデフォルトプロファイルが適用されます。
      </p>
    </div>
  )
}
