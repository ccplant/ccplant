'use client'

import type { useSessionProfileEditor } from './useSessionProfileEditor'
import type { SessionProfileEditorProps } from './SessionProfileEditor'
import ProfileConnectionFields from './ProfileConnectionFields'

type Props = Pick<
  ReturnType<typeof useSessionProfileEditor>,
  | 'githubRepositories'
  | 'setGitHubRepositories'
  | 'codexConnection'
  | 'setCodexConnection'
  | 'claudeConnection'
  | 'setClaudeConnection'
  | 'piConnection'
  | 'setPiConnection'
  | 'codexAuthMode'
  | 'setCodexAuthMode'
  | 'claudeAuthMode'
  | 'setClaudeAuthMode'
  | 'setDirty'
> &
  Pick<SessionProfileEditorProps, never>

export function ProfileAuthenticationSection({
  githubRepositories,
  setGitHubRepositories,
  codexConnection,
  setCodexConnection,
  claudeConnection,
  setClaudeConnection,
  piConnection,
  setPiConnection,
  codexAuthMode,
  setCodexAuthMode,
  claudeAuthMode,
  setClaudeAuthMode,
  setDirty,
}: Props) {
  return (
    <div className="space-y-5">
      <div>
        <label htmlFor="profile-codex-auth" className="block text-sm font-medium mb-1">
          Codex の認証方法
        </label>
        <select
          id="profile-codex-auth"
          value={codexAuthMode}
          onChange={(e) => setCodexAuthMode(e.target.value as typeof codexAuthMode)}
          className="w-full px-3 py-2 text-sm border rounded-lg dark:bg-gray-700 dark:text-white"
        >
          <option value="">ベースの認証方法を使う</option>
          <option value="auth_json">ChatGPT / auth.json</option>
          <option value="openai_compatible">OpenAI 互換 API</option>
        </select>
      </div>
      {codexAuthMode === 'openai_compatible' && (
        <ProfileConnectionFields
          agent="codex"
          value={codexConnection}
          onChange={(value) => {
            setCodexConnection(value)
            setDirty(true)
          }}
        />
      )}
      <div>
        <label htmlFor="profile-claude-auth" className="block text-sm font-medium mb-1">
          Claude Code の認証方法
        </label>
        <select
          id="profile-claude-auth"
          value={claudeAuthMode}
          onChange={(e) => setClaudeAuthMode(e.target.value as typeof claudeAuthMode)}
          className="w-full px-3 py-2 text-sm border rounded-lg dark:bg-gray-700 dark:text-white"
        >
          <option value="">ベースの認証方法を使う</option>
          <option value="oauth">Claude OAuth</option>
          <option value="bedrock">AWS Bedrock</option>
          <option value="anthropic_compatible">Anthropic 互換 API</option>
        </select>
        <p className="text-xs text-gray-500 mt-2">
          認証方法を選ぶと、このプロファイルで使う方法を上書きします。互換 API の接続先・API
          キーも必要に応じて上書きできます。
        </p>
      </div>

      {claudeAuthMode === 'anthropic_compatible' && (
        <ProfileConnectionFields
          agent="claude"
          value={claudeConnection}
          onChange={(value) => {
            setClaudeConnection(value)
            setDirty(true)
          }}
        />
      )}

      <ProfileConnectionFields
        agent="pi"
        value={piConnection}
        onChange={(value) => {
          setPiConnection(value)
          setDirty(true)
        }}
      />

      <div>
        <label htmlFor="profile-github-repositories" className="block text-sm font-medium mb-1">
          クローン可能な追加リポジトリ
        </label>
        <textarea
          id="profile-github-repositories"
          value={githubRepositories}
          onChange={(event) => {
            setGitHubRepositories(event.target.value)
            setDirty(true)
          }}
          rows={5}
          placeholder={'^owner/repository$\n^owner/service-[a-z]+$'}
          className="w-full rounded-md border bg-white p-2 font-mono text-xs dark:bg-gray-800"
        />
        <p className="mt-2 text-xs text-gray-500">
          1行に1つ、RE2正規表現を指定します。リポジトリ名全体に適用され、チーム設定のリストに追加されます。
        </p>
      </div>
    </div>
  )
}
