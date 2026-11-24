'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  | 'name'
  | 'setName'
  | 'description'
  | 'setDescription'
  | 'isDefault'
  | 'setIsDefault'
  | 'repositorySelector'
  | 'setRepositorySelector'
> &
  Pick<SessionProfileEditorProps, never>

export function ProfileBasicSection({
  name,
  setName,
  description,
  setDescription,
  isDefault,
  setIsDefault,
  repositorySelector,
  setRepositorySelector,
}: Props) {
  return (
    <div className="space-y-5">
      {/* Name */}
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          名前 <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="例: my-profile"
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400"
          required
        />
      </div>

      {/* Description */}
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">説明</label>
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="このプロファイルの用途を説明してください"
          rows={2}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y"
        />
      </div>

      {/* is_default */}
      <div>
        <label className="flex items-start gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={isDefault}
            onChange={(e) => setIsDefault(e.target.checked)}
            className="mt-0.5 h-4 w-4 rounded border-gray-300 dark:border-gray-600 text-blue-600 focus:ring-blue-500 cursor-pointer"
          />
          <div>
            <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
              デフォルトプロファイルに設定する
            </span>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
              セッション作成時にこのプロファイルをデフォルトとして使用します。
            </p>
          </div>
        </label>
      </div>

      <div>
        <label
          htmlFor="session-profile-repository-selector"
          className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
        >
          リポジトリ自動選択
        </label>
        <textarea
          id="session-profile-repository-selector"
          value={repositorySelector}
          onChange={(e) => setRepositorySelector(e.target.value)}
          rows={3}
          placeholder={'例: org/repo\norg/another-repo'}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400"
        />
        <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
          1行に1つ owner/repository
          を入力すると、いずれかのリポジトリのセッション起動時にこのプロファイルを自動選択します。
        </p>
      </div>
    </div>
  )
}
