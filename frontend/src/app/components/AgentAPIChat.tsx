'use client';

import AskUserQuestionModal from './AskUserQuestionModal';
import SessionListSidebar from './SessionListSidebar';
import ToolExecutionPane from './ToolExecutionPane';
import { AgentAPIChatProps } from './agentChatPresentation';

import { AgentChatComposer } from './AgentChatComposer';
import { AgentChatFontSettings } from './AgentChatFontSettings';
import { AgentChatHeader } from './AgentChatHeader';
import { AgentChatLinksModal } from './AgentChatLinksModal';
import { AgentChatMessages } from './AgentChatMessages';
import { AgentChatSecretsModal } from './AgentChatSecretsModal';
import { useAgentChat } from './useAgentChat';
export { getACPModelDisplay } from './agentChatPresentation';

export default function AgentAPIChat({ sessionId: propSessionId }: AgentAPIChatProps = {}) {
  const state = useAgentChat({ sessionId: propSessionId });
  const {
    router,
    sessionId,
    isInitialLoadComplete,
    setInputValue,
    agentStatus,
    sidebarVisible,
    templates,
    recentMessages,
    showTemplateModal,
    setShowTemplateModal,
    pendingAction,
    showQuestionModal,
    acpInfo,
    handleAnswerSubmit,
    handleQuestionModalClose,
  } = state;
  return (
    <div className="flex flex-col h-full bg-white dark:bg-gray-900" style={{ position: 'relative', minHeight: 0 }}>
      {/* ── Unified full-width header ── */}
      <AgentChatHeader {...state} />
      {/* ── Body: sidebar + chat content ── */}
      <div className="flex-1 flex overflow-hidden min-h-0">
        {/* Session list sidebar (desktop only — hidden on mobile) */}
        {sessionId && (
          <div className="hidden md:flex">
            <SessionListSidebar currentSessionId={sessionId} isVisible={sidebarVisible} />
          </div>
        )}
        {/* Chat content column */}
        <div className="flex-1 flex flex-col min-w-0 min-h-0">
          {/* Messages */}
          <AgentChatMessages {...state} />

          {/* ツール実行確認ペーン */}
          {/* tool_status is a legacy-agent endpoint. Waiting for bootstrap avoids an
          unnecessary tunnel request while ACP transport/history is being detected. */}
          {sessionId && isInitialLoadComplete && !acpInfo && (
            <ToolExecutionPane sessionId={sessionId} agentStatus={agentStatus?.status} />
          )}

          {/* Input */}
          <AgentChatComposer {...state} />
        </div>{' '}
        {/* end: Chat content column */}
      </div>{' '}
      {/* end: Body (sidebar + chat) */}
      {/* Template Selection Modal */}
      <AgentChatSecretsModal {...state} />
      {showTemplateModal && (
        <div
          className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
          onClick={(e) => {
            if (e.target === e.currentTarget) {
              setShowTemplateModal(false);
            }
          }}
        >
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-2xl w-full max-h-[80vh] overflow-hidden">
            <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white">テンプレートから選択</h2>
                <button
                  onClick={() => setShowTemplateModal(false)}
                  className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            </div>

            <div className="overflow-y-auto max-h-[calc(80vh-8rem)]">
              {/* Recent Messages Section */}
              {recentMessages.length > 0 && (
                <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
                  <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">最近のメッセージ</h3>
                  <div className="space-y-2">
                    {recentMessages.map((message, index) => (
                      <button
                        key={index}
                        onClick={() => {
                          setInputValue(message);
                          setShowTemplateModal(false);
                        }}
                        className="w-full text-left px-3 py-2 bg-gray-50 dark:bg-gray-700 hover:bg-gray-100 dark:hover:bg-gray-600 rounded-md transition-colors"
                      >
                        <div className="text-sm text-gray-900 dark:text-white line-clamp-2">{message}</div>
                      </button>
                    ))}
                  </div>
                </div>
              )}

              {/* Templates Section */}
              <div className="px-6 py-4">
                <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">テンプレート</h3>
                {templates.length > 0 ? (
                  <div className="space-y-2">
                    {templates.map((template) => (
                      <button
                        key={template.id}
                        onClick={() => {
                          setInputValue(template.content);
                          setShowTemplateModal(false);
                        }}
                        className="w-full text-left px-3 py-3 bg-gray-50 dark:bg-gray-700 hover:bg-gray-100 dark:hover:bg-gray-600 rounded-md transition-colors"
                      >
                        <div className="font-medium text-sm text-gray-900 dark:text-white">{template.name}</div>
                        <div className="text-sm text-gray-600 dark:text-gray-400 mt-1 line-clamp-2">
                          {template.content}
                        </div>
                      </button>
                    ))}
                  </div>
                ) : (
                  <div className="text-center py-8 text-gray-500 dark:text-gray-400">
                    <p className="text-sm">テンプレートがありません</p>
                    <button
                      onClick={() => {
                        setShowTemplateModal(false);
                        router.push('/settings?tab=templates');
                      }}
                      className="mt-2 text-sm text-blue-600 dark:text-blue-400 hover:underline"
                    >
                      テンプレートを作成
                    </button>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      )}
      {/* Related Links Modal */}
      <AgentChatLinksModal {...state} />
      {/* Font Settings Popup */}
      <AgentChatFontSettings {...state} />
      {/* AskUserQuestion Modal */}
      {showQuestionModal && pendingAction?.content?.questions && (
        <AskUserQuestionModal
          questions={pendingAction.content.questions}
          onSubmit={handleAnswerSubmit}
          onClose={handleQuestionModalClose}
        />
      )}
    </div>
  );
}
