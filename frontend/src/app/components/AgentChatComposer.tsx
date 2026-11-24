"use client";

import HerdrTerminalPanel from "./HerdrTerminalPanel";

import { AgentChatControls } from "./AgentChatControls";
import { AgentChatMessageComposer } from "./AgentChatMessageComposer";
import { AgentChatSessionInfo } from "./AgentChatSessionInfo";
import type { useAgentChat } from "./useAgentChat";

export function AgentChatComposer(state: ReturnType<typeof useAgentChat>) {
  const {
    sessionId,
    agentAPI,
    selectedHerdrTerminal,
    setSelectedHerdrTerminal,
    herdrRefreshToken,
  } = state;
  return (
    <div className="bg-white dark:bg-gray-900 border-t border-gray-200 dark:border-gray-700 px-4 sm:px-6 py-3 flex-shrink-0">
      {/* Control Panel */}
      <AgentChatControls {...state} />

      <AgentChatSessionInfo state={state} />

      {sessionId && (
        <HerdrTerminalPanel
          client={agentAPI}
          sessionId={sessionId}
          selectedTerminal={selectedHerdrTerminal}
          onSelectTerminal={setSelectedHerdrTerminal}
          refreshToken={herdrRefreshToken}
        />
      )}

      <AgentChatMessageComposer state={state} />
    </div>
  );
}
