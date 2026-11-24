import { AgentAPIProxyError } from '../../lib/agentapi-proxy-client';
import {
  getLatestACPUserPromptIndex,
  getSessionFromList,
  isValidSessionMessageResponse,
  normalizeAgentStatus,
  withACPHistoryMessageIds,
} from './agentChatPresentation';
import { mergeRefreshedMessageHistory } from './messageHistory';
import type { useAgentChat } from './useAgentChat';

export async function loadPreviousACPTurnAction(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    | 'sessionId'
    | 'agentAPIRef'
    | 'setMessages'
    | 'setHasMoreMessages'
    | 'acpUserPrompts'
    | 'setACPUserPrompts'
    | 'isLoadingACPPromptHistory'
    | 'setIsLoadingACPPromptHistory'
    | 'setLoadedACPStartPromptIndex'
    | 'loadedACPStartPromptIndexRef'
    | 'messagesContainerRef'
  > & { acpSessionId?: string },
): Promise<void> {
  const {
    sessionId,
    agentAPIRef,
    setMessages,
    setHasMoreMessages,
    acpSessionId,
    acpUserPrompts,
    setACPUserPrompts,
    isLoadingACPPromptHistory,
    setIsLoadingACPPromptHistory,
    setLoadedACPStartPromptIndex,
    loadedACPStartPromptIndexRef,
    messagesContainerRef,
  } = context;

  if (!sessionId || !agentAPIRef.current) return;

  if (!acpSessionId) return;
  if (isLoadingACPPromptHistory) return;

  const currentStartIndex = loadedACPStartPromptIndexRef.current ?? getLatestACPUserPromptIndex(acpUserPrompts);
  if (currentStartIndex === null || currentStartIndex <= 0) return;
  const previousPromptIndex = currentStartIndex - 1;
  const container = messagesContainerRef.current;
  const previousScrollHeight = container?.scrollHeight ?? 0;
  const previousScrollTop = container?.scrollTop ?? 0;
  const restoreScrollPosition = () => {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        if (!container) return;
        const scrollDiff = container.scrollHeight - previousScrollHeight;
        container.scrollTop = previousScrollTop + scrollDiff;
      });
    });
  };

  setIsLoadingACPPromptHistory(true);
  try {
    const result = await agentAPIRef.current.getACPMessageHistory(sessionId, acpSessionId, previousPromptIndex);
    const olderMessages = withACPHistoryMessageIds(result.messages, previousPromptIndex);
    setMessages((prev) => [...olderMessages, ...prev]);
    setACPUserPrompts(result.userPrompts);
    setLoadedACPStartPromptIndex(previousPromptIndex);
    setHasMoreMessages(false);
    restoreScrollPosition();
  } catch (err) {
    console.warn('[ACP] Failed to load previous prompt history:', err);
  } finally {
    setIsLoadingACPPromptHistory(false);
  }
}

export async function loadMoreMessagesAction(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    | 'sessionId'
    | 'agentAPIRef'
    | 'messages'
    | 'setMessages'
    | 'hasMoreMessages'
    | 'setHasMoreMessages'
    | 'isLoadingMore'
    | 'setIsLoadingMore'
    | 'messagesContainerRef'
    | 'lastLoadTimeRef'
  >,
): Promise<void> {
  const {
    sessionId,
    agentAPIRef,
    messages,
    setMessages,
    hasMoreMessages,
    setHasMoreMessages,
    isLoadingMore,
    setIsLoadingMore,
    messagesContainerRef,
    lastLoadTimeRef,
  } = context;

  if (!hasMoreMessages || isLoadingMore || !sessionId || !agentAPIRef.current) return;

  // Prevent loading if less than 2 seconds have passed since last load
  const now = Date.now();
  const timeSinceLastLoad = now - lastLoadTimeRef.current;
  const cooldownPeriod = 2000; // 2 seconds

  if (timeSinceLastLoad < cooldownPeriod) {
    console.log(
      `[loadMoreMessages] Cooldown active. Wait ${Math.ceil((cooldownPeriod - timeSinceLastLoad) / 1000)}s before loading more.`,
    );
    return;
  }

  const container = messagesContainerRef.current;
  if (!container) return;

  // Update last load time
  lastLoadTimeRef.current = now;

  // Save current scroll position
  const previousScrollHeight = container.scrollHeight;
  const previousScrollTop = container.scrollTop;

  setIsLoadingMore(true);
  try {
    // Get the oldest message ID (smallest ID)
    const oldestMessage = messages[0];
    if (!oldestMessage || typeof oldestMessage.id !== 'number') {
      console.error('Invalid oldest message:', oldestMessage);
      setIsLoadingMore(false);
      setHasMoreMessages(false);
      return;
    }

    console.log('[loadMoreMessages] Fetching messages before ID:', oldestMessage.id);

    // Fetch messages before the oldest message ID using cursor-based pagination
    const response = await agentAPIRef.current.getSessionMessages(sessionId, {
      before: oldestMessage.id,
      limit: 50,
    });

    if (!isValidSessionMessageResponse(response)) {
      console.warn('Invalid session messages response:', response);
      setIsLoadingMore(false);
      return;
    }

    const olderMessages = response?.messages || [];
    console.log(`[loadMoreMessages] Fetched ${olderMessages.length} older messages`);

    if (olderMessages.length > 0) {
      // Prepend older messages to the beginning
      setMessages((prev) => [...olderMessages, ...prev]);

      // Use hasMore field from response to determine if there are more messages
      const hasMore = response?.hasMore ?? olderMessages.length >= 50;
      setHasMoreMessages(hasMore);

      // Restore scroll position after DOM update
      setTimeout(() => {
        if (container) {
          const newScrollHeight = container.scrollHeight;
          const scrollDiff = newScrollHeight - previousScrollHeight;
          container.scrollTop = previousScrollTop + scrollDiff;
          console.log('[loadMoreMessages] Restored scroll position, scrollDiff:', scrollDiff);
        }
      }, 0);
    } else {
      console.log('[loadMoreMessages] No more older messages');
      setHasMoreMessages(false);
    }
  } catch (err) {
    console.error('Failed to load more messages:', err);
  } finally {
    setIsLoadingMore(false);
  }
}

export async function pollMessagesAction(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    | 'sessionId'
    | 'agentAPIRef'
    | 'messages'
    | 'setMessages'
    | 'setAgentStatus'
    | 'setError'
    | 'isConnected'
    | 'setSessionAnnotations'
    | 'setSessionModelOptions'
    | 'agentType'
    | 'pendingAction'
    | 'setPendingAction'
    | 'setShowQuestionModal'
    | 'prevAgentStatusRef'
    | 'lastSessionAnnotationLoadTimeRef'
  >,
): Promise<void> {
  const {
    sessionId,
    agentAPIRef,
    messages,
    setMessages,
    setAgentStatus,
    setError,
    isConnected,
    setSessionAnnotations,
    setSessionModelOptions,
    agentType,
    pendingAction,
    setPendingAction,
    setShowQuestionModal,
    prevAgentStatusRef,
    lastSessionAnnotationLoadTimeRef,
  } = context;

  if (!isConnected || !sessionId || !agentAPIRef.current) return;

  try {
    // Get the latest message ID for polling new messages only (claude agent only)
    const latestMessage = messages[messages.length - 1];
    const latestMessageId = latestMessage && typeof latestMessage.id === 'number' ? latestMessage.id : undefined;

    // Poll messages, status, pending actions, and session annotations.
    // For claude agents: fetch only new messages using 'after' parameter
    // For other agents: always fetch all messages to ensure timeline is updated
    const shouldRefreshAnnotations = Date.now() - lastSessionAnnotationLoadTimeRef.current > 5000;
    const [sessionMessagesResponse, sessionStatus, pendingActions, sessionListResponse] = await Promise.all([
      agentAPIRef.current.getSessionMessages(
        sessionId,
        agentType === 'claude' && latestMessageId !== undefined
          ? {
              after: latestMessageId, // Get messages with ID > latestMessageId (newer messages)
              limit: 50,
            }
          : {
              limit: 50,
              direction: 'tail', // Get latest 50 messages
            },
      ),
      agentAPIRef.current.getSessionStatus(sessionId),
      agentAPIRef.current.getPendingActions(sessionId),
      shouldRefreshAnnotations ? agentAPIRef.current.search({ limit: 100 }) : Promise.resolve(null),
    ]);

    // Validate and safely handle session messages response
    if (!isValidSessionMessageResponse(sessionMessagesResponse)) {
      console.warn('Invalid session messages response structure during polling:', sessionMessagesResponse);
      return;
    }

    // Use SessionMessage directly for display
    const newMessages = sessionMessagesResponse?.messages || [];

    // Handle message updates based on agent type
    if (newMessages.length > 0) {
      if (agentType === 'claude') {
        // For claude agents: filter and append only truly new messages
        setMessages((prevMessages) => {
          const existingIds = new Set(prevMessages.map((m) => m.id));
          const trulyNewMessages = newMessages.filter((m) => !existingIds.has(m.id));

          if (trulyNewMessages.length === 0) return prevMessages;

          return [...prevMessages, ...trulyNewMessages];
        });
      } else {
        // For other agents: always update with all messages to reflect timeline changes
        setMessages((prevMessages) => mergeRefreshedMessageHistory(prevMessages, newMessages));
      }
    }

    // Handle pending actions
    const questionAction = pendingActions.find((a) => a.type === 'answer_question');
    if (questionAction && !pendingAction) {
      setPendingAction(questionAction);
      setShowQuestionModal(true);
    } else if (!questionAction && pendingAction) {
      // Question was answered/cleared
      setPendingAction(null);
      setShowQuestionModal(false);
    }

    const normalizedSessionStatus = { ...sessionStatus, status: normalizeAgentStatus(sessionStatus.status) };
    setAgentStatus(normalizedSessionStatus);
    if (sessionListResponse) {
      const session = getSessionFromList(sessionListResponse.sessions, sessionId);
      setSessionAnnotations(session?.annotations);
      setSessionModelOptions(session?.model_options ?? []);
      lastSessionAnnotationLoadTimeRef.current = Date.now();
    }
    // When the provisioner has permanently failed, surface the error once.
    if (sessionStatus.status === 'error' && prevAgentStatusRef.current?.status !== 'error') {
      if (sessionStatus.error_code === 'codex_auth_required') {
        setError('Codex の認証期限が切れています。再認証してください。');
      } else {
        const detail = sessionStatus.message || '不明なエラー';
        setError(`セッションの起動に失敗しました: ${detail}`);
      }
    }
    prevAgentStatusRef.current = normalizedSessionStatus;
  } catch (err) {
    console.error('Failed to poll session data:', err);
    if (err instanceof AgentAPIProxyError) {
      if (err.status === 502 || err.status === 503) {
        // サービス一時停止の可能性があるため、エラーを表示せず継続
        console.warn('Received 502/503 during polling, continuing...');
      } else {
        setError(`Failed to update messages: ${err.message}`);
      }
    }
  }
}
