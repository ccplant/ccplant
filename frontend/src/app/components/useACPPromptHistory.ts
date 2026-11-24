import { useCallback, useEffect, useRef, useState, type Dispatch, type MutableRefObject, type RefObject, type SetStateAction, type TouchEvent } from "react";
import type { ACPUserPromptInfo, createAgentAPIProxyClientFromStorage } from "../../lib/agentapi-proxy-client";
import type { SessionMessage } from "../../types/agentapi";
import { loadPreviousACPTurnAction } from "./agentChatHistoryActions";
import { getLatestACPUserPromptIndex } from "./agentChatPresentation";

type AgentClient = ReturnType<typeof createAgentAPIProxyClientFromStorage>;

interface PromptHistoryOptions {
  sessionId: string | null;
  acpSessionId?: string;
  isACPSession: boolean;
  agentAPIRef: MutableRefObject<AgentClient>;
  messagesContainerRef: RefObject<HTMLDivElement | null>;
  setMessages: Dispatch<SetStateAction<SessionMessage[]>>;
  setHasMoreMessages: Dispatch<SetStateAction<boolean>>;
}

function useACPPullGesture(containerRef: RefObject<HTMLDivElement | null>, canLoadPrevious: () => boolean, loadPrevious: () => Promise<unknown>) {
  const [acpPullDistance, setACPPullDistance] = useState(0);
  const startYRef = useRef<number | null>(null);
  const trackingRef = useRef(false);
  const distanceRef = useRef(0);
  const handleMessagesTouchStart = useCallback(
    (event: TouchEvent<HTMLDivElement>) => {
      const container = containerRef.current;
      if (!container || container.scrollTop > 0 || !canLoadPrevious()) {
        trackingRef.current = false;
        startYRef.current = null;
        return;
      }
      trackingRef.current = true;
      startYRef.current = event.touches[0]?.clientY ?? null;
    },
    [canLoadPrevious, containerRef],
  );
  const handleMessagesTouchMove = useCallback(
    (event: TouchEvent<HTMLDivElement>) => {
      if (!trackingRef.current || startYRef.current === null) return;
      const currentY = event.touches[0]?.clientY;
      if (!containerRef.current || containerRef.current.scrollTop > 0 || currentY === undefined) return;
      const distance = Math.min(Math.max(currentY - startYRef.current, 0) * 0.55, 96);
      distanceRef.current = distance;
      setACPPullDistance(distance);
      if (distance > 8) event.preventDefault();
    },
    [containerRef],
  );
  const finishMessagesPull = useCallback(() => {
    const shouldLoad = trackingRef.current && distanceRef.current >= 72;
    trackingRef.current = false;
    startYRef.current = null;
    distanceRef.current = 0;
    setACPPullDistance(0);
    if (shouldLoad) void loadPrevious();
  }, [loadPrevious]);
  return {
    acpPullDistance,
    setACPPullDistance,
    acpPullStartYRef: startYRef,
    acpPullTrackingRef: trackingRef,
    acpPullDistanceRef: distanceRef,
    acpPullThreshold: 72,
    handleMessagesTouchStart,
    handleMessagesTouchMove,
    finishMessagesPull,
  };
}

export function useACPPromptHistory(options: PromptHistoryOptions) {
  const [acpUserPrompts, setACPUserPrompts] = useState<ACPUserPromptInfo[]>([]);
  const [isLoadingACPPromptHistory, setIsLoadingACPPromptHistory] = useState(false);
  const [loadedACPStartPromptIndex, setLoadedACPStartPromptIndex] = useState<number | null>(null);
  const loadedIndexRef = useRef<number | null>(null);
  useEffect(() => {
    loadedIndexRef.current = loadedACPStartPromptIndex;
  }, [loadedACPStartPromptIndex]);
  const loadPreviousACPTurn = useCallback(
    () =>
      loadPreviousACPTurnAction({
        sessionId: options.sessionId,
        agentAPIRef: options.agentAPIRef,
        setMessages: options.setMessages,
        setHasMoreMessages: options.setHasMoreMessages,
        acpSessionId: options.acpSessionId,
        acpUserPrompts,
        setACPUserPrompts,
        isLoadingACPPromptHistory,
        setIsLoadingACPPromptHistory,
        setLoadedACPStartPromptIndex,
        loadedACPStartPromptIndexRef: loadedIndexRef,
        messagesContainerRef: options.messagesContainerRef,
      }),
    [acpUserPrompts, isLoadingACPPromptHistory, options],
  );
  const canLoadPreviousACPTurn = useCallback(() => {
    if (!options.isACPSession || acpUserPrompts.length <= 1 || isLoadingACPPromptHistory) return false;
    const start = loadedIndexRef.current ?? getLatestACPUserPromptIndex(acpUserPrompts);
    return start !== null && start > 0;
  }, [acpUserPrompts, isLoadingACPPromptHistory, options.isACPSession]);
  const pullGesture = useACPPullGesture(options.messagesContainerRef, canLoadPreviousACPTurn, loadPreviousACPTurn);
  return {
    acpUserPrompts,
    setACPUserPrompts,
    isLoadingACPPromptHistory,
    setIsLoadingACPPromptHistory,
    loadedACPStartPromptIndex,
    setLoadedACPStartPromptIndex,
    loadedACPStartPromptIndexRef: loadedIndexRef,
    loadPreviousACPTurn,
    canLoadPreviousACPTurn,
    ...pullGesture,
  };
}
