import { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import type { createAgentAPIProxyClientFromStorage } from "../../lib/agentapi-proxy-client";
import type { SessionAnnotations } from "../../types/agentapi";
import { getSessionFromList, SIDEBAR_VISIBLE_KEY } from "./agentChatPresentation";

type AgentClient = ReturnType<typeof createAgentAPIProxyClientFromStorage>;

export function useAgentChatSidebar() {
  const [sidebarVisible, setSidebarVisible] = useState(false);
  useEffect(() => {
    setSidebarVisible(localStorage.getItem(SIDEBAR_VISIBLE_KEY) !== "false");
  }, []);
  const toggleSidebar = useCallback(() => {
    setSidebarVisible((current) => {
      const next = !current;
      localStorage.setItem(SIDEBAR_VISIBLE_KEY, String(next));
      return next;
    });
  }, []);
  return { sidebarVisible, setSidebarVisible, toggleSidebar };
}

export function useAgentChatAnnotations(isInitialLoadComplete: boolean, sessionId: string | null, agentAPIRef: RefObject<AgentClient>) {
  const [sessionAnnotations, setSessionAnnotations] = useState<SessionAnnotations>();
  const [sessionModelOptions, setSessionModelOptions] = useState<string[]>([]);
  const lastSessionAnnotationLoadTimeRef = useRef(0);
  useEffect(() => {
    if (!isInitialLoadComplete || !sessionId) return;
    const timeoutId = setTimeout(() => {
      void agentAPIRef.current
        .search({ limit: 100 })
        .then((response) => {
          const session = getSessionFromList(response.sessions, sessionId);
          setSessionAnnotations(session?.annotations);
          setSessionModelOptions(session?.model_options ?? []);
          lastSessionAnnotationLoadTimeRef.current = Date.now();
        })
        .catch((error) => console.warn("Failed to load session annotations:", error));
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [agentAPIRef, isInitialLoadComplete, sessionId]);
  return {
    sessionAnnotations,
    setSessionAnnotations,
    sessionModelOptions,
    setSessionModelOptions,
    lastSessionAnnotationLoadTimeRef,
  };
}
