import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import {
  createSharedSessionClientFromStorage,
  SharedSessionClientError,
} from "../../lib/shared-session-client";
import type {
  SessionMessage,
  SessionMessageListResponse,
} from "../../types/agentapi";
import { useBackgroundAwareInterval } from "../hooks/usePageVisibility";
import type {
  SharedAgentStatus,
  SharedSessionMessage,
} from "./SharedSessionChatView";

type SharedClient = ReturnType<typeof createSharedSessionClientFromStorage>;

function isMessageResponse(
  response: unknown,
): response is SessionMessageListResponse {
  return (
    response !== null &&
    typeof response === "object" &&
    "messages" in response &&
    Array.isArray((response as SessionMessageListResponse).messages)
  );
}

function numericMessageId(id: string | number, fallbackId: number): number {
  const stringId = String(id);
  if (!stringId.trim()) return fallbackId;
  const parsed = Number.parseInt(stringId, 10);
  if (!Number.isNaN(parsed) && parsed >= 0) return parsed;
  const hash = [...stringId].reduce(
    (value, character) => (value << 5) - value + character.charCodeAt(0),
    0,
  );
  return Math.abs(hash) || fallbackId;
}

function convertMessages(messages: SessionMessage[]): SharedSessionMessage[] {
  const now = Date.now();
  return messages.map((message, index) => ({
    id: numericMessageId(message.id, now + index),
    role: message.role === "user" ? "user" : "agent",
    content: message.content,
    timestamp: message.time || message.timestamp || new Date().toISOString(),
  }));
}

function initializationError(error: unknown): string {
  if (!(error instanceof SharedSessionClientError))
    return "共有セッションへの接続に失敗しました";
  const messages: Record<string, string> = {
    SHARE_NOT_FOUND:
      "共有セッションが見つかりません。URLが正しいか確認してください。",
    SHARE_EXPIRED: "この共有リンクは有効期限が切れています。",
    SHARE_FORBIDDEN: "この共有セッションへのアクセス権限がありません。",
  };
  return (
    messages[error.code] ||
    `共有セッションの読み込みに失敗しました: ${error.message}`
  );
}

function messagesChanged(
  previous: SharedSessionMessage[],
  next: SharedSessionMessage[],
): boolean {
  if (previous.length !== next.length) return true;
  return next.some((message, index) => {
    const old = previous[index];
    return (
      !old ||
      old.id !== message.id ||
      old.role !== message.role ||
      old.content !== message.content ||
      old.timestamp !== message.timestamp
    );
  });
}

interface SessionDataState {
  messages: SharedSessionMessage[];
  agentStatus: SharedAgentStatus | null;
  error: string | null;
  isConnected: boolean;
}

interface SessionUpdaters {
  setMessages: (messages: SharedSessionMessage[]) => void;
  setError: (error: string | null) => void;
  setIsConnected: (connected: boolean) => void;
}

function useInitializeSharedSession(
  client: SharedClient,
  shareToken: string,
  update: SessionUpdaters,
) {
  useEffect(() => {
    if (!shareToken) return;
    const initialize = async () => {
      update.setError(null);
      update.setIsConnected(true);
      try {
        const response = await client.getMessages(shareToken, { limit: 100 });
        if (!isMessageResponse(response))
          console.warn(
            "Invalid session messages response structure:",
            response,
          );
        update.setMessages(convertMessages(response?.messages || []));
      } catch (error) {
        console.error("Failed to load shared session messages:", error);
        update.setIsConnected(false);
        update.setError(initializationError(error));
      }
    };
    const timeoutId = setTimeout(initialize, 0);
    return () => clearTimeout(timeoutId);
  }, [client, shareToken, update]);
}

function usePollSharedSession(
  client: SharedClient,
  shareToken: string,
  state: SessionDataState,
  setState: Dispatch<SetStateAction<SessionDataState>>,
) {
  const poll = useCallback(async () => {
    if (!state.isConnected || !shareToken) return;
    try {
      const [response, status] = await Promise.all([
        client.getMessages(shareToken, { limit: 100 }),
        client.getStatus(shareToken),
      ]);
      if (!isMessageResponse(response)) {
        console.warn(
          "Invalid session messages response during polling:",
          response,
        );
        return;
      }
      const messages = convertMessages(response.messages || []);
      setState((current) => ({
        ...current,
        messages: messagesChanged(current.messages, messages)
          ? messages
          : current.messages,
        agentStatus: status as SharedAgentStatus,
      }));
    } catch (error) {
      console.error("Failed to poll shared session data:", error);
      if (
        error instanceof SharedSessionClientError &&
        error.code === "SHARE_EXPIRED"
      ) {
        setState((current) => ({
          ...current,
          error: "この共有リンクは有効期限が切れています。",
          isConnected: false,
        }));
      }
    }
  }, [client, shareToken, state.isConnected, setState]);
  const control = useBackgroundAwareInterval(poll, 2000, true);
  const controlRef = useRef(control);
  controlRef.current = control;
  useEffect(() => {
    const current = controlRef.current;
    if (state.isConnected && shareToken) current.start();
    else current.stop();
    return () => current.stop();
  }, [shareToken, state.isConnected]);
}

export function useSharedSessionData(shareToken: string): SessionDataState {
  const [client] = useState(createSharedSessionClientFromStorage);
  const [state, setState] = useState<SessionDataState>({
    messages: [],
    agentStatus: null,
    error: null,
    isConnected: false,
  });
  const update = useMemo<SessionUpdaters>(
    () => ({
      setMessages: (messages) =>
        setState((current) => ({ ...current, messages })),
      setError: (error) => setState((current) => ({ ...current, error })),
      setIsConnected: (isConnected) =>
        setState((current) => ({ ...current, isConnected })),
    }),
    [],
  );
  useInitializeSharedSession(client, shareToken, update);
  usePollSharedSession(client, shareToken, state, setState);
  return state;
}

export function useSharedSessionScroll(messageCount: number) {
  const [shouldAutoScroll, setShouldAutoScroll] = useState(true);
  const [hasNewMessages, setHasNewMessages] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const messagesContainerRef = useRef<HTMLDivElement>(null);
  const previousCount = useRef(0);
  const scrollToBottom = useCallback(
    () => messagesEndRef.current?.scrollIntoView({ behavior: "smooth" }),
    [],
  );
  const handleScroll = useCallback(() => {
    const container = messagesContainerRef.current;
    if (!container) return;
    const remaining =
      container.scrollHeight - container.scrollTop - container.clientHeight;
    const isAtBottom = remaining <= 100;
    setShouldAutoScroll(isAtBottom);
    if (isAtBottom) setHasNewMessages(false);
  }, []);
  useEffect(() => {
    if (messageCount > previousCount.current) {
      if (shouldAutoScroll) scrollToBottom();
      else setHasNewMessages(true);
    }
    previousCount.current = messageCount;
  }, [messageCount, scrollToBottom, shouldAutoScroll]);
  return {
    shouldAutoScroll,
    setShouldAutoScroll,
    hasNewMessages,
    setHasNewMessages,
    messagesEndRef,
    messagesContainerRef,
    scrollToBottom,
    handleScroll,
  };
}
