import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { ACPServerClient } from '../../lib/acp-server-client';
import type {
  ACPConfigOption,
  ACPSessionInfo,
  ACPUserPromptInfo,
  createAgentAPIProxyClientFromStorage,
} from '../../lib/agentapi-proxy-client';
import type { PendingAction, SessionMessage } from '../../types/agentapi';
import { createAgentChatStreamCallbacks } from './agentChatStreamCallbacks';
import {
  type AgentStatus,
  getLatestACPUserPromptIndex,
  type MessageSSEConnectionStatus,
  normalizeAgentStatus,
  withACPHistoryMessageIds,
} from './agentChatPresentation';
import {
  canRebuildCompleteHistoryWindow,
  mergeRefreshedMessageHistory,
  replaceRebuiltMessageHistory,
} from './messageHistory';

type AgentAPIClient = ReturnType<typeof createAgentAPIProxyClientFromStorage>;
type EventSourceRef = MutableRefObject<{ close: () => void } | null>;

export interface ReconnectVisibleACPContext {
  sessionId: string;
  acpInfo: ACPSessionInfo;
  agentAPI: AgentAPIClient;
  agentAPIRef: MutableRefObject<AgentAPIClient>;
  acpServerEnabled: boolean;
  acpServerClientRef: MutableRefObject<ACPServerClient | null>;
  acpEventSourceRef: EventSourceRef;
  loadedACPStartPromptIndexRef: MutableRefObject<number | null>;
  setMessages: Dispatch<SetStateAction<SessionMessage[]>>;
  setLoadedACPStartPromptIndex: Dispatch<SetStateAction<number | null>>;
  setACPUserPrompts: Dispatch<SetStateAction<ACPUserPromptInfo[]>>;
  setAgentStatus: Dispatch<SetStateAction<AgentStatus | null>>;
  setMessageSSEConnectionStatus: Dispatch<SetStateAction<MessageSSEConnectionStatus>>;
  applyACPConfigOptions: (configOptions: ACPConfigOption[]) => void;
  setACPPendingPermission: Dispatch<SetStateAction<{ action: PendingAction; rpcId: number } | null>>;
  setPendingAction: Dispatch<SetStateAction<PendingAction | null>>;
  setShowQuestionModal: Dispatch<SetStateAction<boolean>>;
}

async function refreshVisibleACPHistory(context: ReconnectVisibleACPContext) {
  const { agentAPI, sessionId, acpInfo, loadedACPStartPromptIndexRef } = context;
  const latestResult = await agentAPI.getACPMessageHistory(sessionId, acpInfo.sessionId);
  const latestPromptIndex = latestResult.userPromptIndex ?? getLatestACPUserPromptIndex(latestResult.userPrompts);
  const currentStartIndex = loadedACPStartPromptIndexRef.current;

  if (latestPromptIndex !== null && canRebuildCompleteHistoryWindow(currentStartIndex, latestPromptIndex)) {
    const historicalTurns = await Promise.all(
      Array.from({ length: latestPromptIndex - currentStartIndex }, (_, offset) => {
        const promptIndex = currentStartIndex + offset;
        return agentAPI
          .getACPMessageHistory(sessionId, acpInfo.sessionId, promptIndex)
          .then((result) => withACPHistoryMessageIds(result.messages, promptIndex));
      }),
    );
    const refreshedMessages = [...historicalTurns.flat(), ...latestResult.messages];
    context.setMessages((messages) => replaceRebuiltMessageHistory(messages, refreshedMessages));
    context.setLoadedACPStartPromptIndex(currentStartIndex);
  } else {
    context.setMessages((messages) => mergeRefreshedMessageHistory(messages, latestResult.messages));
    context.setLoadedACPStartPromptIndex(latestPromptIndex);
  }
  context.setACPUserPrompts(latestResult.userPrompts);
}

async function refreshVisibleAgentStatus(context: ReconnectVisibleACPContext) {
  try {
    const currentStatus = await context.agentAPI.getSessionStatus(context.sessionId);
    context.setAgentStatus({ ...currentStatus, status: normalizeAgentStatus(currentStatus.status) });
  } catch (error) {
    console.warn('[ACP] Failed to refresh agent status on visibility change:', error);
  }
}

function reconnectVisibleACPStream(context: ReconnectVisibleACPContext) {
  context.acpEventSourceRef.current?.close();
  context.setMessageSSEConnectionStatus('connecting');
  const callbacks = {
    ...createAgentChatStreamCallbacks(context),
    onError: (error: Event | Error) => {
      console.error('[ACP] SSE error callback (from visibility reconnect):', error);
    },
  };

  context.acpEventSourceRef.current =
    context.acpServerEnabled && context.acpServerClientRef.current
      ? context.acpServerClientRef.current.subscribeToEvents(context.sessionId, callbacks)
      : context.agentAPIRef.current.subscribeToACPSessionEvents(context.sessionId, context.acpInfo.sessionId, {
          ...callbacks,
          onCommandsUpdate: (commands) => console.log('[ACP] available_commands_update:', commands),
        });
}

export async function reconnectVisibleACP(context: ReconnectVisibleACPContext) {
  console.log('[ACP] Page became visible, refreshing messages and checking SSE connection...');
  try {
    await refreshVisibleACPHistory(context);
  } catch (error) {
    console.warn('[ACP] Failed to refresh message history on visibility change:', error);
  }
  await refreshVisibleAgentStatus(context);
  reconnectVisibleACPStream(context);
}
