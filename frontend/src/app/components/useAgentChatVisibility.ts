import { MutableRefObject, useEffect, useRef } from "react";
import { ACPSessionInfo } from "../../lib/agentapi-proxy-client";
import { usePageVisibility } from "../hooks/usePageVisibility";
import { ReconnectVisibleACPContext, reconnectVisibleACP } from "./agentChatVisibilityActions";

type VisibilityConfig = Omit<ReconnectVisibleACPContext, "sessionId" | "acpInfo" | "agentAPI"> & {
  sessionId: string | null;
  acpInfo: ACPSessionInfo | null;
  acpStatusEventSourceRef: MutableRefObject<{ close: () => void } | null>;
};

export function useAgentChatVisibility(config: VisibilityConfig) {
  const { sessionId, acpInfo, agentAPIRef, acpEventSourceRef, acpStatusEventSourceRef } = config;
  const isPageVisible = usePageVisibility();
  const prevIsPageVisibleRef = useRef(true);

  useEffect(() => {
    return () => {
      acpEventSourceRef.current?.close();
      acpEventSourceRef.current = null;
      acpStatusEventSourceRef.current?.close();
      acpStatusEventSourceRef.current = null;
    };
  }, [acpEventSourceRef, acpStatusEventSourceRef, sessionId]);

  useEffect(() => {
    const wasHidden = !prevIsPageVisibleRef.current;
    prevIsPageVisibleRef.current = isPageVisible;
    if (!isPageVisible || !wasHidden || !acpInfo || !sessionId || !agentAPIRef.current) return;

    void reconnectVisibleACP({
      ...config,
      sessionId,
      acpInfo,
      agentAPI: agentAPIRef.current,
    });
  }, [acpInfo, agentAPIRef, config, isPageVisible, sessionId]);

  return { isPageVisible, prevIsPageVisibleRef };
}
