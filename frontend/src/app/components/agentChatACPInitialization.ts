import {
  getLatestACPUserPromptIndex,
  isACPAgentType,
  normalizeAgentStatus,
} from "./agentChatPresentation";
import { createAgentChatStreamCallbacks } from "./agentChatStreamCallbacks";
import type { AgentChatInitializationContext } from "./agentChatInitialization";

export async function initializeACPChat(
  context: AgentChatInitializationContext,
  retry: () => void,
): Promise<boolean> {
  const probe = await createACPProbe(context);
  if (!probe) return false;
  if (probe.info) {
    await restoreACPChat(context, probe);
    return true;
  }
  if (context.acpServerEnabled && context.acpServerClientRef.current) {
    enterEarlyACPMode(context, probe.callbacks);
    return true;
  }
  return checkPendingACPBridge(context, retry);
}

async function createACPProbe(context: AgentChatInitializationContext) {
  const { sessionId, agentAPIRef, acpServerEnabled, acpEventSourceRef } =
    context;
  const client = agentAPIRef.current;
  if (!client || !sessionId) return null;
  const callbacks = createAgentChatStreamCallbacks(context);
  let historyError: unknown = null;
  const historyPromise = client
    .getACPMessageHistory(sessionId, "")
    .catch((error) => {
      historyError = error;
      return null;
    });
  const subscription = acpServerEnabled
    ? null
    : client.subscribeToACPSessionEventsForInitialization(sessionId, {
        ...callbacks,
        onCommandsUpdate: (commands) =>
          console.log("[ACP] available_commands_update:", commands),
      });
  if (subscription) {
    acpEventSourceRef.current?.close();
    acpEventSourceRef.current = subscription;
  }
  const available = await (subscription?.opened ?? Promise.resolve(false));
  if (subscription && !available) {
    subscription.close();
    if (acpEventSourceRef.current === subscription)
      acpEventSourceRef.current = null;
  }
  const info = available
    ? { sessionId: "", status: "running" as const }
    : await client.getACPSessionInfo(sessionId);
  return {
    callbacks,
    historyPromise,
    getHistoryError: () => historyError,
    subscription,
    available,
    info,
  };
}

type ACPProbe = NonNullable<Awaited<ReturnType<typeof createACPProbe>>>;

function subscribeToACPStatus(context: AgentChatInitializationContext) {
  const {
    sessionId,
    agentAPIRef,
    acpStatusEventSourceRef,
    setAgentStatus,
    setACPUserPrompts,
    setLoadedACPStartPromptIndex,
  } = context;
  if (!agentAPIRef.current || !sessionId) return;
  acpStatusEventSourceRef.current?.close();
  acpStatusEventSourceRef.current = agentAPIRef.current.subscribeToACPStatus(
    sessionId,
    (status) => {
      setAgentStatus(status);
      if (status.status !== "stable") return;
      void agentAPIRef.current
        ?.getACPMessageHistory(sessionId, "")
        .then((result) => {
          setACPUserPrompts(result.userPrompts);
          setLoadedACPStartPromptIndex(
            (previous) =>
              previous ??
              result.userPromptIndex ??
              getLatestACPUserPromptIndex(result.userPrompts),
          );
        });
    },
    (error) => console.warn("[ACP] Status SSE error:", error),
  );
}

async function restoreACPChat(
  context: AgentChatInitializationContext,
  probe: ACPProbe,
) {
  const { sessionId, acpInfo, setACPInfo, setAgentType } = context;
  if (!sessionId || !probe.info) return;
  console.log(
    `[ACP] initializeChat: ACP session detected (acpSessionId=${probe.info.sessionId}), previous acpInfo=${JSON.stringify(acpInfo)}`,
  );
  setACPInfo(probe.info);
  setAgentType("acp");
  subscribeToACPStatus(context);
  if (probe.available) hydrateACPInfoAfterHistory(context, probe);
  const history = await probe.historyPromise;
  if (!history) {
    throw (
      probe.getHistoryError() ??
      new Error("Failed to restore ACP message history")
    );
  }
  applyACPHistory(context, history);
  connectACPEvents(context, probe, history.lastEventId);
}

function hydrateACPInfoAfterHistory(
  context: AgentChatInitializationContext,
  probe: ACPProbe,
) {
  const { sessionId, agentAPIRef, setACPInfo } = context;
  if (!sessionId) return;
  void probe.historyPromise
    .then(() => agentAPIRef.current?.getACPSessionInfo(sessionId))
    .then((info) => {
      if (info) setACPInfo(info);
    });
}

function applyACPHistory(
  context: AgentChatInitializationContext,
  history: NonNullable<Awaited<ACPProbe["historyPromise"]>>,
) {
  context.setMessages(history.messages);
  context.setACPUserPrompts(history.userPrompts);
  context.setLoadedACPStartPromptIndex(
    history.userPromptIndex ?? getLatestACPUserPromptIndex(history.userPrompts),
  );
  context.setHasMoreMessages(false);
  context.setIsInitialLoadComplete(true);
  context.setIsStarting(false);
  console.log(
    `[ACP] initializeChat: restored ${history.messages.length} messages from bridge history`,
  );
}

function connectACPEvents(
  context: AgentChatInitializationContext,
  probe: ACPProbe,
  lastEventId?: number,
) {
  const {
    sessionId,
    agentAPIRef,
    acpServerEnabled,
    acpServerClientRef,
    acpEventSourceRef,
    setMessageSSEConnectionStatus,
  } = context;
  if (!sessionId || !agentAPIRef.current) return;
  if (probe.subscription && probe.available) {
    probe.subscription.resumeAfterHistory(lastEventId);
    return;
  }
  setMessageSSEConnectionStatus("connecting");
  if (acpServerEnabled && acpServerClientRef.current) {
    acpEventSourceRef.current = acpServerClientRef.current.subscribeToEvents(
      sessionId,
      probe.callbacks,
      lastEventId,
    );
    return;
  }
  acpEventSourceRef.current = agentAPIRef.current.subscribeToACPSessionEvents(
    sessionId,
    probe.info?.sessionId || "",
    {
      ...probe.callbacks,
      onCommandsUpdate: (commands) =>
        console.log("[ACP] available_commands_update:", commands),
    },
    lastEventId,
  );
}

function enterEarlyACPMode(
  context: AgentChatInitializationContext,
  callbacks: ReturnType<typeof createAgentChatStreamCallbacks>,
) {
  const {
    sessionId,
    acpServerClientRef,
    acpEventSourceRef,
    setACPInfo,
    setAgentType,
    setIsInitialLoadComplete,
    setIsStarting,
    setMessageSSEConnectionStatus,
  } = context;
  if (!sessionId || !acpServerClientRef.current) return;
  console.log(
    `[ACP] initializeChat: bridge not ready, entering early ACP mode (sessionId=${sessionId})`,
  );
  setACPInfo({ sessionId: "", status: "running" });
  setAgentType("acp");
  subscribeToACPStatus(context);
  setIsInitialLoadComplete(true);
  setIsStarting(false);
  if (!acpEventSourceRef.current) {
    setMessageSSEConnectionStatus("connecting");
    acpEventSourceRef.current = acpServerClientRef.current.subscribeToEvents(
      sessionId,
      callbacks,
    );
  }
}

async function checkPendingACPBridge(
  context: AgentChatInitializationContext,
  retry: () => void,
) {
  const { sessionId, agentAPIRef, setAgentStatus, setError, setIsStarting } =
    context;
  if (!sessionId || !agentAPIRef.current) return false;
  try {
    const status = await agentAPIRef.current
      .getSessionStatus(sessionId)
      .catch(() => null);
    if (!status) return false;
    setAgentStatus({ ...status, status: normalizeAgentStatus(status.status) });
    if (!isACPAgentType(status.agent_type)) return false;
    if (status.status === "error") {
      setError(
        `セッションの起動に失敗しました: ${status.message || "不明なエラー"}`,
      );
      setIsStarting(false);
      return true;
    }
    console.log(
      `[ACP] initializeChat: ${status.agent_type} bridge not ready, retrying (sessionId=${sessionId})`,
    );
    setIsStarting(true);
    retry();
    return true;
  } catch (error) {
    console.debug(
      "[ACP] Session status unavailable during initialization; falling back to history",
      error,
    );
    return false;
  }
}
