import { AgentAPIProxyError } from "../../lib/agentapi-proxy-client";
import { isValidSessionMessageResponse } from "./agentChatPresentation";
import { initializeACPChat } from "./agentChatACPInitialization";
import type { useAgentChat } from "./useAgentChat";

export type AgentChatInitializationContext = Pick<
  ReturnType<typeof useAgentChat>,
  | "agentAPI"
  | "sessionId"
  | "setIsInitialLoadComplete"
  | "setIsStarting"
  | "setIsResuming"
  | "setIsConnected"
  | "setAgentStatus"
  | "setAgentType"
  | "setMessages"
  | "setACPInfo"
  | "setACPUserPrompts"
  | "setLoadedACPStartPromptIndex"
  | "setMessageSSEConnectionStatus"
  | "retryTimerRef"
  | "acpInfo"
  | "acpEventSourceRef"
  | "setError"
  | "agentAPIRef"
  | "applyACPConfigOptions"
  | "setACPPendingPermission"
  | "setPendingAction"
  | "setShowQuestionModal"
  | "acpStatusEventSourceRef"
  | "acpServerEnabled"
  | "setHasMoreMessages"
  | "acpServerClientRef"
  | "messagesEndRef"
>;

export function initializeAgentChat(
  context: AgentChatInitializationContext,
): (() => void) | undefined {
  if (!context.agentAPI || !context.sessionId) return undefined;
  resetAgentChatInitialization(context);
  const run = () => void initializeChat(context);
  const timeoutId = setTimeout(run, 0);
  return () => {
    clearTimeout(timeoutId);
    clearInitializationRetry(context);
  };
}

function resetAgentChatInitialization(context: AgentChatInitializationContext) {
  context.setIsInitialLoadComplete(false);
  context.setIsStarting(true);
  context.setIsResuming(false);
  context.setIsConnected(false);
  context.setAgentStatus(null);
  context.setAgentType(null);
  context.setMessages([]);
  context.setACPInfo(null);
  context.setACPUserPrompts([]);
  context.setLoadedACPStartPromptIndex(null);
  context.setMessageSSEConnectionStatus("connecting");
  clearInitializationRetry(context);
}

function clearInitializationRetry(context: AgentChatInitializationContext) {
  if (!context.retryTimerRef.current) return;
  clearTimeout(context.retryTimerRef.current);
  context.retryTimerRef.current = null;
}

function scheduleInitializationRetry(context: AgentChatInitializationContext) {
  context.retryTimerRef.current = setTimeout(
    () => void initializeChat(context),
    2000,
  );
}

async function initializeChat(context: AgentChatInitializationContext) {
  const { sessionId, acpInfo, acpEventSourceRef, setError, setIsConnected } =
    context;
  console.log(
    `[ACP] initializeChat called (sessionId=${sessionId}, existingACPInfo=${JSON.stringify(acpInfo)}, hasExistingEventSource=${!!acpEventSourceRef.current})`,
  );
  try {
    setError(null);
    setIsConnected(true);
    if (
      await initializeACPChat(context, () =>
        scheduleInitializationRetry(context),
      )
    )
      return;
    await initializeLegacyChat(context);
  } catch (error) {
    handleInitializationError(context, error);
  }
}

async function initializeLegacyChat(context: AgentChatInitializationContext) {
  const { sessionId, agentAPIRef } = context;
  if (!agentAPIRef.current || !sessionId) return;
  try {
    await loadLegacyMessages(context);
  } catch (error) {
    await handleLegacyMessageError(context, error);
  }
}

async function loadLegacyMessages(context: AgentChatInitializationContext) {
  const {
    sessionId,
    agentAPIRef,
    setMessages,
    setHasMoreMessages,
    setError,
    setIsStarting,
    setIsInitialLoadComplete,
    messagesEndRef,
  } = context;
  if (!agentAPIRef.current || !sessionId) return;
  const response = await agentAPIRef.current.getSessionMessages(sessionId, {
    limit: 50,
    direction: "tail",
  });
  if (!isValidSessionMessageResponse(response)) {
    console.warn("Invalid session messages response structure:", response);
  }
  const messages = response?.messages || [];
  setMessages(messages);
  setHasMoreMessages(
    response?.hasMore ??
      (response?.total ? response.total > messages.length : false),
  );
  setError(null);
  setIsStarting(false);
  setIsInitialLoadComplete(true);
  setTimeout(
    () => messagesEndRef.current?.scrollIntoView({ behavior: "auto" }),
    100,
  );
}

async function handleLegacyMessageError(
  context: AgentChatInitializationContext,
  error: unknown,
) {
  const { sessionId, setIsConnected, setIsResuming, setError } = context;
  console.error("Failed to load session messages:", error);
  setIsConnected(false);
  if (isRetryableInitializationError(error)) {
    setIsResuming((current) => current || error.code === "session_resuming");
    if (await provisioningPermanentlyFailed(context)) return;
    context.setIsStarting(true);
    scheduleInitializationRetry(context);
    return;
  }
  setError(
    error instanceof AgentAPIProxyError
      ? `セッションメッセージの読み込みに失敗しました: ${error.message} (セッション: ${sessionId})`
      : `セッション ${sessionId} への接続に失敗しました`,
  );
}

async function provisioningPermanentlyFailed(
  context: AgentChatInitializationContext,
) {
  const { agentAPIRef, sessionId, setError, setIsStarting } = context;
  if (!agentAPIRef.current || !sessionId) return false;
  try {
    const status = await agentAPIRef.current.getSessionStatus(sessionId);
    if (status.status !== "error") return false;
    setError(
      `セッションの起動に失敗しました: ${status.message || "不明なエラー"}`,
    );
    setIsStarting(false);
    return true;
  } catch (statusError) {
    console.debug(
      "[ACP] Provisioner status unavailable; continuing normal retry",
      statusError,
    );
    return false;
  }
}

function isRetryableInitializationError(
  error: unknown,
): error is AgentAPIProxyError {
  return (
    error instanceof AgentAPIProxyError &&
    [404, 502, 503].includes(error.status)
  );
}

function handleInitializationError(
  context: AgentChatInitializationContext,
  error: unknown,
) {
  const { setIsConnected, setIsResuming, setIsStarting, setError } = context;
  console.error("Failed to initialize chat:", error);
  setIsConnected(false);
  if (
    error instanceof AgentAPIProxyError &&
    [404, 502, 503].includes(error.status)
  ) {
    setIsResuming((current) => current || error.code === "session_resuming");
    setIsStarting(true);
    scheduleInitializationRetry(context);
    return;
  }
  if (error instanceof AgentAPIProxyError && error.status === 423) {
    setError(
      "セッションは停止中、または設定を再読み込み中です。セッション一覧で進行状況を確認し、停止中の場合は「設定を再読み込みして再起動」を選んでください。",
    );
    return;
  }
  setError(
    error instanceof AgentAPIProxyError
      ? `接続に失敗しました: ${error.message}`
      : "AgentAPI Proxyへの接続に失敗しました",
  );
}
