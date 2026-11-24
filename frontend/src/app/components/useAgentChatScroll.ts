import { useCallback, useRef, useState } from "react";
import type { SessionMessage } from "../../types/agentapi";
import { useMessageAutoScroll } from "./useAgentChatUIEffects";

export function useAgentChatScroll(messages: SessionMessage[]) {
  const [shouldAutoScroll, setShouldAutoScroll] = useState(true);
  const [hasNewMessages, setHasNewMessages] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const messagesContainerRef = useRef<HTMLDivElement>(null);
  const messagesTopRef = useRef<HTMLDivElement>(null);
  const previousLengthRef = useRef(0);
  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);
  const checkIfAtBottom = useCallback(() => {
    const container = messagesContainerRef.current;
    if (!container) return false;
    const remaining = container.scrollHeight - container.scrollTop - container.clientHeight;
    return remaining <= 100;
  }, []);
  const handleScroll = useCallback(() => {
    const isAtBottom = checkIfAtBottom();
    setShouldAutoScroll(isAtBottom);
    if (isAtBottom) setHasNewMessages(false);
  }, [checkIfAtBottom]);
  useMessageAutoScroll({
    messages,
    shouldAutoScroll,
    messagesEndRef,
    previousLengthRef,
    setHasNewMessages,
  });
  return {
    shouldAutoScroll,
    setShouldAutoScroll,
    hasNewMessages,
    setHasNewMessages,
    messagesEndRef,
    messagesContainerRef,
    messagesTopRef,
    prevMessagesLengthRef: previousLengthRef,
    scrollToBottom,
    checkIfAtBottom,
    handleScroll,
  };
}
