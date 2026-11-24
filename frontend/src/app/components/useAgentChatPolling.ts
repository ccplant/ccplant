import { useEffect } from "react";
import { ACPSessionInfo } from "../../lib/agentapi-proxy-client";
import { useBackgroundAwareInterval } from "../hooks/usePageVisibility";

interface AgentChatPollingConfig {
  pollMessages: () => Promise<void>;
  isConnected: boolean;
  isInitialLoadComplete: boolean;
  sessionId: string | null;
  acpInfo: ACPSessionInfo | null;
}

export function useAgentChatPolling({ pollMessages, isConnected, isInitialLoadComplete, sessionId, acpInfo }: AgentChatPollingConfig) {
  const pollingControl = useBackgroundAwareInterval(pollMessages, 1000, false);

  useEffect(() => {
    if (isConnected && isInitialLoadComplete && sessionId && !acpInfo) {
      pollingControl.start();
    } else {
      pollingControl.stop();
    }
    return () => pollingControl.stop();
  }, [acpInfo, isConnected, isInitialLoadComplete, pollingControl, sessionId]);

  return pollingControl;
}
