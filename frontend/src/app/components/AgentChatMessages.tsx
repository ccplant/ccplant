'use client';

import type { useAgentChat } from './useAgentChat';
import { ChatHistoryControls, ChatMessageList, ChatScrollControls, ChatStatus } from './AgentChatMessageParts';

export function AgentChatMessages(state: ReturnType<typeof useAgentChat>) {
  return (
    <div
      ref={state.messagesContainerRef}
      onScroll={state.handleScroll}
      onTouchStart={state.handleMessagesTouchStart}
      onTouchMove={state.handleMessagesTouchMove}
      onTouchEnd={state.finishMessagesPull}
      onTouchCancel={state.finishMessagesPull}
      className="flex-1 overflow-y-auto bg-white dark:bg-gray-900 mobile-scroll min-h-0 relative"
      style={{ overscrollBehavior: 'contain', WebkitOverflowScrolling: 'touch', transform: 'translateZ(0)' }}
    >
      <ChatHistoryControls state={state} />
      <ChatStatus state={state} />
      {state.isLoadingMore && <div className="flex justify-center items-center py-4"><div className="animate-spin rounded-full h-6 w-6 border-b-2 border-blue-600" /><span className="ml-2 text-sm text-gray-600 dark:text-gray-400">読み込み中...</span></div>}
      <div ref={state.messagesTopRef} style={{ height: '1px' }} />
      <ChatMessageList state={state} />
      <div ref={state.messagesEndRef} />
      <ChatScrollControls state={state} />
    </div>
  );
}
