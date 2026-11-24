'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'
import { SkillSettings } from '../../components/settings/SkillSettings'

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  'skills' | 'setSkills' | 'piPackages' | 'setPiPackages' | 'setDirty'
> &
  Pick<SessionProfileEditorProps, never>

export function ProfileSkillsSection({ skills, setSkills, piPackages, setPiPackages, setDirty }: Props) {
  return (
    <div className="space-y-5">
      <div>
        <p className="mb-3 text-xs text-gray-500 dark:text-gray-400">
          個人・チーム設定の Skills に、このプロファイルの package source を追加します。実際の導入先はセッションの agent
          type から Claude Code または Codex の一方だけが選ばれます。
        </p>
        <SkillSettings
          skills={skills}
          onChange={(value) => {
            setSkills(value)
            setDirty(true)
          }}
        />
        <div className="mt-6">
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Pi packages</label>
          <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
            Pi 起動前に導入する package source を1行ずつ指定します。Git管理のハーネスは{' '}
            <code>git:github.com/org/pi-harness@v1</code> のように指定できます。
          </p>
          <textarea
            value={piPackages}
            onChange={(event) => {
              setPiPackages(event.target.value)
              setDirty(true)
            }}
            rows={4}
            placeholder={'git:github.com/org/pi-harness@v1'}
            className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-800 dark:text-white"
          />
        </div>
      </div>
    </div>
  )
}
