import { useCallback, useEffect, useState, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import type { createAgentAPIProxyClientFromStorage } from "../../lib/agentapi-proxy-client";

type AgentClient = ReturnType<typeof createAgentAPIProxyClientFromStorage>;

interface HerdrOptions {
  sessionId: string | null;
  agentAPIRef: MutableRefObject<AgentClient>;
  inputValue: string;
  setInputValue: Dispatch<SetStateAction<string>>;
  isConnected: boolean;
  setError: Dispatch<SetStateAction<string | null>>;
  sendMessage: () => Promise<void>;
}

export function useAgentChatHerdr(options: HerdrOptions) {
  const [selectedHerdrTerminal, setSelectedHerdrTerminal] = useState("");
  const [isHerdrActionPending, setIsHerdrActionPending] = useState(false);
  const [herdrRefreshToken, setHerdrRefreshToken] = useState(0);
  useEffect(() => setSelectedHerdrTerminal(""), [options.sessionId]);
  const sendComposerMessage = useCallback(async () => {
    if (!selectedHerdrTerminal) return options.sendMessage();
    const content = options.inputValue.trim();
    if (!content || !options.sessionId || isHerdrActionPending || !options.isConnected) return;
    setIsHerdrActionPending(true);
    options.setError(null);
    try {
      await options.agentAPIRef.current.sendHerdrTerminal(options.sessionId, selectedHerdrTerminal, content);
      options.setInputValue("");
      setHerdrRefreshToken((current) => current + 1);
    } catch (error) {
      options.setError(`Herdr エージェントへの送信に失敗しました: ${error instanceof Error ? error.message : "不明なエラー"}`);
    } finally {
      setIsHerdrActionPending(false);
    }
  }, [isHerdrActionPending, options, selectedHerdrTerminal]);
  const interruptSelectedHerdr = useCallback(async () => {
    if (!selectedHerdrTerminal || !options.sessionId || isHerdrActionPending) return;
    setIsHerdrActionPending(true);
    options.setError(null);
    try {
      await options.agentAPIRef.current.interruptHerdrTerminal(options.sessionId, selectedHerdrTerminal);
      setHerdrRefreshToken((current) => current + 1);
    } catch (error) {
      options.setError(`Herdr エージェントを中断できませんでした: ${error instanceof Error ? error.message : "不明なエラー"}`);
    } finally {
      setIsHerdrActionPending(false);
    }
  }, [isHerdrActionPending, options, selectedHerdrTerminal]);
  return {
    selectedHerdrTerminal,
    setSelectedHerdrTerminal,
    isHerdrActionPending,
    setIsHerdrActionPending,
    herdrRefreshToken,
    setHerdrRefreshToken,
    sendComposerMessage,
    interruptSelectedHerdr,
  };
}
