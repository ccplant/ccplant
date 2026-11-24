'use client'
import SessionCreationProgressModal from '../../components/SessionCreationProgressModal'
import TopBar from '../../components/TopBar'
import { NewSessionAgentTypeFields } from './NewSessionAgentTypeFields'
import { NewSessionCheckoutFields } from './NewSessionCheckoutFields'
import { NewSessionConnectionFields } from './NewSessionConnectionFields'
import { NewSessionCycleFields } from './NewSessionCycleFields'
import { NewSessionDockerFields } from './NewSessionDockerFields'
import { NewSessionMessageFields } from './NewSessionMessageFields'
import { NewSessionPoolFields } from './NewSessionPoolFields'
import { getAgentTypeLabel } from './newSessionPresentation'
import { NewSessionProfileField } from './NewSessionProfileField'
import { NewSessionRepositoryFields } from './NewSessionRepositoryFields'
import { NewSessionTemplateModal } from './NewSessionTemplateModal'
import { NewSessionTTLField } from './NewSessionTTLField'
import type { NewSessionFormState } from './useNewSession'
export function NewSessionView(context: NewSessionFormState) {
  const {
    initialMessage,
    isCreating,
    error,
    statusMessage,
    showTemplateModal,
    showProgressModal,
    creationProgress,
    showAdvancedSettings,
    setShowAdvancedSettings,
    selectedAgentType,
    availablePools,
    selectedManagerId,
    cycleEnabled,
    cycleMessage,
    cycleMaxCount,
    linkedConnections,
    connectionId,
    handleSubmit,
    handleCloseProgressModal,
    handleRetry,
    handleCancel,
  } = context
  return (
    <main className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <TopBar title="新しいセッション" showSettingsButton={true} />

      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        <div className="bg-white dark:bg-gray-800 rounded-xl shadow-sm border border-gray-200 dark:border-gray-700">
          {/* ヘッダー */}
          <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
            <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">新しいセッションを開始</h1>
            <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">セッションの設定を入力してください</p>
          </div>

          {/* フォーム */}
          <form onSubmit={handleSubmit} className="p-6 space-y-6">
            {/* 初期メッセージ */}
            <NewSessionMessageFields {...context} />

            {/* リポジトリ入力 */}
            <NewSessionRepositoryFields {...context} />

            {/* Checkout 指定 */}
            <NewSessionCheckoutFields {...context} />

            {/* セッションプロファイル */}
            <NewSessionProfileField {...context} />

            {/* その他の設定 */}
            <div>
              <button
                type="button"
                onClick={() => setShowAdvancedSettings(!showAdvancedSettings)}
                className="flex items-center gap-1.5 text-xs text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300 transition-colors disabled:opacity-40"
                disabled={isCreating}
              >
                <svg
                  className={`w-3 h-3 transition-transform ${showAdvancedSettings ? 'rotate-180' : ''}`}
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                </svg>
                その他の設定
                {selectedAgentType !== 'default' && (
                  <span className="px-1.5 py-0.5 bg-blue-100 dark:bg-blue-900/40 text-blue-600 dark:text-blue-400 text-xs rounded-full">
                    {getAgentTypeLabel(selectedAgentType)}
                  </span>
                )}
                {selectedManagerId !== '' && (
                  <span className="px-1.5 py-0.5 bg-green-100 dark:bg-green-900/40 text-green-600 dark:text-green-400 text-xs rounded-full">
                    {availablePools.find((pool) => pool.name === selectedManagerId)?.name ?? 'カスタム'}
                  </span>
                )}
                {connectionId !== '' && (
                  <span className="px-1.5 py-0.5 bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 text-xs rounded-full">
                    {linkedConnections.find((identity) => identity.connection_id === connectionId)?.connection_name ??
                      'アカウント接続'}
                  </span>
                )}
                {cycleEnabled && cycleMessage.trim() && (
                  <span className="px-1.5 py-0.5 bg-purple-100 dark:bg-purple-900/40 text-purple-600 dark:text-purple-400 text-xs rounded-full">
                    サイクル{cycleMaxCount > 0 ? ` (${cycleMaxCount}回)` : ''}
                  </span>
                )}
              </button>

              {showAdvancedSettings && (
                <div className="mt-3 pl-1 space-y-4">
                  {/* 外部アカウント接続 */}
                  <NewSessionConnectionFields {...context} />

                  {/* 実行Pool選択 */}
                  {availablePools.length > 0 && <NewSessionPoolFields {...context} />}
                  {/* サイクルセッション */}
                  <NewSessionCycleFields {...context} />

                  {/* Docker in Docker (DinD) */}
                  <NewSessionDockerFields {...context} />

                  {/* セッション TTL */}
                  <NewSessionTTLField {...context} />

                  {/* エージェントタイプ */}
                  <NewSessionAgentTypeFields {...context} />
                </div>
              )}
            </div>

            {/* エラー表示 */}
            {error && (
              <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4">
                <p className="text-red-600 dark:text-red-400 text-sm">{error}</p>
              </div>
            )}

            {/* ステータス表示 */}
            {statusMessage && (
              <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
                <p className="text-blue-600 dark:text-blue-400 text-sm">{statusMessage}</p>
              </div>
            )}

            {/* ボタン */}
            <div className="flex justify-end gap-4 pt-4 border-t border-gray-200 dark:border-gray-700">
              <button
                type="button"
                onClick={handleCancel}
                className="px-6 py-3 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-lg transition-colors"
              >
                キャンセル
              </button>
              <button
                type="submit"
                disabled={!initialMessage.trim() || isCreating}
                className="px-6 py-3 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 disabled:bg-gray-400 disabled:cursor-not-allowed rounded-lg transition-colors flex items-center"
              >
                {isCreating ? (
                  <>
                    <svg
                      className="animate-spin -ml-1 mr-2 h-4 w-4 text-white"
                      xmlns="http://www.w3.org/2000/svg"
                      fill="none"
                      viewBox="0 0 24 24"
                    >
                      <circle
                        className="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        strokeWidth="4"
                      ></circle>
                      <path
                        className="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                      ></path>
                    </svg>
                    作成中...
                  </>
                ) : (
                  'セッション開始'
                )}
              </button>
            </div>
          </form>
        </div>
      </div>

      {/* Template Selection Modal */}
      {showTemplateModal && <NewSessionTemplateModal {...context} />}

      {/* Session Creation Progress Modal */}
      {showProgressModal && creationProgress && (
        <SessionCreationProgressModal
          isOpen={showProgressModal}
          progress={creationProgress}
          onClose={creationProgress.status === 'failed' ? handleCloseProgressModal : undefined}
          onRetry={creationProgress.status === 'failed' ? handleRetry : undefined}
        />
      )}
    </main>
  )
}
