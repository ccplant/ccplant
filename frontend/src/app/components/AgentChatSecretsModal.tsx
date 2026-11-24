'use client';

import type { useAgentChat } from './useAgentChat';

export function AgentChatSecretsModal(state: ReturnType<typeof useAgentChat>) {
  const {
    showSecretModal,
    secretValue,
    setSecretValue,
    secretLifetime,
    setSecretLifetime,
    isRegisteringSecret,
    sessionSecrets,
    isLoadingSessionSecrets,
    registerOneTimeSecret,
    closeSecretModal,
    reauthorizeOneTimeSecret,
  } = state;
  return (
    <>
      {showSecretModal && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black bg-opacity-50 p-4"
          onClick={(event) => {
            if (event.target === event.currentTarget) closeSecretModal();
          }}
        >
          <div className="w-full max-w-lg rounded-lg bg-white shadow-xl dark:bg-gray-800">
            <div className="flex items-center justify-between border-b border-gray-200 px-6 py-4 dark:border-gray-700">
              <h2 className="text-lg font-semibold text-gray-900 dark:text-white">秘匿情報を一度だけ渡す</h2>
              <button
                type="button"
                onClick={closeSecretModal}
                disabled={isRegisteringSecret}
                aria-label="閉じる"
                className="text-gray-400 hover:text-gray-600 disabled:cursor-not-allowed dark:hover:text-gray-200"
              >
                ×
              </button>
            </div>
            <div className="space-y-4 px-6 py-5">
              <p className="text-sm text-gray-600 dark:text-gray-300">
                値はチャット履歴やブラウザには保存されません。エージェントが取得すると取得済みになり、人間が許可するまで再取得できません。
              </p>
              {isLoadingSessionSecrets && <p className="text-sm text-gray-500">登録済み情報を確認中…</p>}
              {sessionSecrets
                .filter((secret) => secret.status === 'consumed')
                .map((secret) => (
                  <div
                    key={secret.secret_id}
                    className="space-y-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100"
                  >
                    <p>
                      取得済みの秘匿情報（{new Date(secret.consumed_at || secret.created_at).toLocaleString()}
                      ）を、もう一度だけ取得できるようにします。
                    </p>
                    <button
                      type="button"
                      onClick={() => void reauthorizeOneTimeSecret(secret.secret_id)}
                      disabled={isRegisteringSecret}
                      className="rounded-md border border-amber-500 px-3 py-2 font-medium hover:bg-amber-100 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-amber-900"
                    >
                      {isRegisteringSecret ? '許可中…' : '再取得を許可'}
                    </button>
                  </div>
                ))}
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-200">
                秘匿情報
                <textarea
                  value={secretValue}
                  onChange={(event) => setSecretValue(event.target.value)}
                  rows={5}
                  maxLength={65536}
                  autoComplete="off"
                  spellCheck={false}
                  autoFocus
                  aria-label="一度だけ渡す秘匿情報"
                  className="mt-1 w-full resize-y rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm text-gray-900 focus:border-amber-500 focus:outline-none focus:ring-2 focus:ring-amber-500 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-100"
                />
              </label>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-200">
                有効期限
                <select
                  value={secretLifetime}
                  onChange={(event) => setSecretLifetime(Number(event.target.value))}
                  className="mt-1 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-100"
                >
                  <option value={600}>10分</option>
                  <option value={1800}>30分</option>
                  <option value={3600}>60分</option>
                </select>
              </label>
            </div>
            <div className="flex justify-end gap-2 border-t border-gray-200 px-6 py-4 dark:border-gray-700">
              <button
                type="button"
                onClick={closeSecretModal}
                disabled={isRegisteringSecret}
                className="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
              >
                キャンセル
              </button>
              <button
                type="button"
                onClick={() => void registerOneTimeSecret()}
                disabled={!secretValue || isRegisteringSecret}
                className="rounded-md bg-amber-600 px-4 py-2 text-sm font-medium text-white hover:bg-amber-700 disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600"
              >
                {isRegisteringSecret ? '登録中…' : '一度だけ渡す'}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
