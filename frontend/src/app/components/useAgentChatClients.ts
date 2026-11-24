import { useEffect, useRef, useState } from "react";
import { createACPServerClientFromStorage } from "../../lib/acp-server-client";
import { createAgentAPIProxyClientFromStorage } from "../../lib/agentapi-proxy-client";
import { getACPServerEnabled } from "../../types/settings";
import { pushNotificationManager } from "../../utils/pushNotification";

export function useAgentChatClients() {
  const [agentAPI] = useState(createAgentAPIProxyClientFromStorage);
  const agentAPIRef = useRef(agentAPI);
  useEffect(() => {
    agentAPIRef.current = agentAPI;
  }, [agentAPI]);
  const [acpServerEnabled] = useState(() => typeof window !== "undefined" && getACPServerEnabled());
  const acpServerClientRef = useRef<ReturnType<typeof createACPServerClientFromStorage> | null>(null);
  useEffect(() => {
    if (!acpServerEnabled) return;
    const client = createACPServerClientFromStorage();
    acpServerClientRef.current = client;
    void client.initialize().catch((error) => console.warn("[ACP] initialize handshake failed:", error));
  }, [acpServerEnabled]);
  useEffect(() => {
    void pushNotificationManager.initialize().catch(console.error);
  }, []);
  return { agentAPI, agentAPIRef, acpServerEnabled, acpServerClientRef };
}
