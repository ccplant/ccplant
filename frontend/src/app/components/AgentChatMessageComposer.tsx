"use client";

import { AgentChatComposerActions } from "./AgentChatComposerActions";
import { AgentChatComposerEditor } from "./AgentChatComposerEditor";
import type { useAgentChat } from "./useAgentChat";

export function AgentChatMessageComposer({
  state,
}: {
  state: ReturnType<typeof useAgentChat>;
}) {
  return (
    <div className="flex items-start space-x-2 sm:space-x-3">
      <div className="flex-shrink-0">
        <div className="w-7 h-7 sm:w-8 sm:h-8 rounded-full bg-blue-500 text-white flex items-center justify-center">
          <svg
            className="w-3 h-3 sm:w-4 sm:h-4"
            fill="currentColor"
            viewBox="0 0 24 24"
          >
            <path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z" />
          </svg>
        </div>
      </div>
      <div className="relative min-w-0 flex-1">
        <AgentChatComposerEditor state={state} />
        <AgentChatComposerActions state={state} />
      </div>
    </div>
  );
}
