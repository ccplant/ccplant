import { type ReactElement } from 'react';
import MessageItem from './MessageItem';
import { isACPAgentType } from './agentChatPresentation';
import type { useAgentChat } from './useAgentChat';

type ChatState = ReturnType<typeof useAgentChat>;

export function ChatHistoryControls({ state }: { state: ChatState }) {
  const visible = state.acpPullDistance > 0 || state.isLoadingACPPromptHistory;
  return <>
    {visible && <div className="pointer-events-none absolute inset-x-0 top-0 z-20 flex h-9 items-center justify-center border-b border-blue-100 bg-blue-50/95 text-xs text-blue-700 shadow-sm dark:border-blue-900/40 dark:bg-blue-950/80 dark:text-blue-200" style={{ transform: state.isLoadingACPPromptHistory ? 'translateY(0)' : `translateY(${Math.max(0, Math.min(state.acpPullDistance, 36)) - 36}px)` }}>
      {state.isLoadingACPPromptHistory ? <span className="inline-flex items-center gap-2"><span className="h-3 w-3 animate-spin rounded-full border-2 border-blue-300 border-t-blue-700 dark:border-blue-700 dark:border-t-blue-200" />読み込み中...</span> : state.acpPullDistance >= state.acpPullThreshold ? '離して読み込み' : '前のターン'}
    </div>}
    {state.canLoadPreviousACPTurn() && <div className="hidden justify-center border-b border-gray-100 bg-white px-4 py-3 md:flex dark:border-gray-800 dark:bg-gray-900">
      <button type="button" onClick={() => void state.loadPreviousACPTurn()} disabled={state.isLoadingACPPromptHistory} className="inline-flex items-center gap-2 rounded-full border border-blue-200 bg-white px-4 py-2 text-sm font-medium text-blue-700 shadow-sm transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-blue-800 dark:bg-gray-900 dark:text-blue-200 dark:hover:bg-blue-950">
        {state.isLoadingACPPromptHistory ? <span className="h-4 w-4 animate-spin rounded-full border-2 border-blue-300 border-t-blue-700 dark:border-blue-700 dark:border-t-blue-200" /> : <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19V5m0 0l-7 7m7-7l7 7" /></svg>}
        {state.isLoadingACPPromptHistory ? '読み込み中...' : '前のターンを読み込む'}
      </button>
    </div>}
  </>;
}

function ErrorStatus({ state }: { state: ChatState }) {
  if (state.agentStatus?.status !== 'error') return null;
  return <div className="flex flex-col items-center justify-center py-12 px-6">
    <div className="mb-4 text-red-500 dark:text-red-400"><svg className="w-14 h-14 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" /></svg></div>
    <p className="text-lg font-semibold text-red-600 dark:text-red-400">セッションの起動に失敗しました</p>
    {state.agentStatus.message && <p className="mt-2 text-sm text-gray-600 dark:text-gray-400 max-w-lg text-center break-words">{state.agentStatus.message}</p>}
    <p className="mt-4 text-xs text-gray-400 dark:text-gray-500">セッションを削除して再作成してください</p>
  </div>;
}

export function ChatStatus({ state }: { state: ChatState }) {
  if (state.agentStatus?.status === 'error') return <ErrorStatus state={state} />;
  if (!state.isInitialLoadComplete) return <div className="mx-auto flex w-full max-w-3xl items-center gap-3 px-4 py-6 text-gray-500 dark:text-gray-400 sm:px-6">
    <div className="h-5 w-5 flex-none animate-spin rounded-full border-2 border-gray-200 border-t-blue-500 dark:border-gray-700 dark:border-t-blue-400" />
    <div className="min-w-0"><p className="text-sm font-medium text-gray-700 dark:text-gray-200">{state.isResuming ? 'セッションを再開しています' : 'チャットを準備しています'}</p><p className="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{state.isResuming ? '保存された会話と実行環境を復元しています。準備ができると自動的に表示されます。' : state.acpInfo ? 'メッセージ履歴を読み込んでいます' : 'セッションへの接続を待機しています'}</p></div>
  </div>;
  if (state.messages.length === 0 && state.isConnected) return <div className="text-center text-gray-500 dark:text-gray-400 py-12"><div className="mb-3"><svg className="w-12 h-12 mx-auto text-gray-300 dark:text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" /></svg></div><p className="text-lg font-medium">No conversation yet</p><p className="text-sm mt-1">Start a conversation with the agent below</p></div>;
  return null;
}

function renderMessages(state: ChatState) {
  const rendered: ReactElement[] = [];
  const processedIds = new Set<number>();
  state.messages.forEach((message) => {
    if (processedIds.has(message.id)) return;
    if (message.role === 'tool_result' && message.parentToolUseId) { processedIds.add(message.id); return; }
    const isToolUse = message.role === 'agent' && message.toolUseId;
    const toolResult = isToolUse ? state.messages.find((item) => item.role === 'tool_result' && item.parentToolUseId === message.toolUseId) : undefined;
    if (toolResult) processedIds.add(toolResult.id);
    const supported = ['user', 'assistant', 'agent', 'tool_result'].includes(message.role);
    if (supported) {
      rendered.push(<MessageItem key={message.id} message={message} toolResult={toolResult} formatTimestamp={state.formatTimestamp} fontSettings={state.fontSettings} isClaudeAgent={state.agentType === 'claude' || state.agentType === 'codex' || isACPAgentType(state.agentType)} />);
      processedIds.add(message.id);
    }
  });
  return rendered;
}

export function ChatMessageList({ state }: { state: ChatState }) {
  return <div className="divide-y divide-gray-200 dark:divide-gray-700">{renderMessages(state)}</div>;
}

export function ChatScrollControls({ state }: { state: ChatState }) {
  const scroll = (clearNew: boolean) => { state.setShouldAutoScroll(true); if (clearNew) state.setHasNewMessages(false); state.scrollToBottom(); };
  if (state.hasNewMessages) return <div className="absolute bottom-4 left-1/2 transform -translate-x-1/2"><button onClick={() => scroll(true)} className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-full shadow-lg flex items-center space-x-2 transition-colors"><span className="text-sm">新しいメッセージ</span><svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 14l-7 7m0 0l-7-7m7 7V3" /></svg></button></div>;
  if (!state.shouldAutoScroll) return <div className="absolute bottom-4 right-4"><button onClick={() => scroll(false)} className="bg-gray-600 hover:bg-gray-700 text-white p-2 rounded-full shadow-lg transition-colors" title="最新メッセージにスクロール"><svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 14l-7 7m0 0l-7-7m7 7V3" /></svg></button></div>;
  return null;
}
