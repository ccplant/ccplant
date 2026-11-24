import { useState, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import type { createAgentAPIProxyClientFromStorage, SessionSecretMetadata } from "../../lib/agentapi-proxy-client";
import { loadSessionSecretsAction, reauthorizeOneTimeSecretAction, registerOneTimeSecretAction } from "./agentChatSessionActions";

type AgentClient = ReturnType<typeof createAgentAPIProxyClientFromStorage>;

export function useAgentChatSecrets(sessionId: string | null, agentAPIRef: MutableRefObject<AgentClient>, setError: Dispatch<SetStateAction<string | null>>) {
  const [showSecretModal, setShowSecretModal] = useState(false);
  const [secretValue, setSecretValue] = useState("");
  const [secretLifetime, setSecretLifetime] = useState(600);
  const [isRegisteringSecret, setIsRegisteringSecret] = useState(false);
  const [sessionSecrets, setSessionSecrets] = useState<SessionSecretMetadata[]>([]);
  const [isLoadingSessionSecrets, setIsLoadingSessionSecrets] = useState(false);
  const [secretFeedback, setSecretFeedback] = useState<string | null>(null);
  const registerOneTimeSecret = () =>
    registerOneTimeSecretAction({
      sessionId,
      agentAPIRef,
      setError,
      setShowSecretModal,
      secretValue,
      setSecretValue,
      secretLifetime,
      setIsRegisteringSecret,
      setSecretFeedback,
    });
  const loadSessionSecrets = () =>
    loadSessionSecretsAction({
      sessionId,
      agentAPIRef,
      setError,
      setSessionSecrets,
      setIsLoadingSessionSecrets,
    });
  const closeSecretModal = () => {
    if (isRegisteringSecret) return;
    setSecretValue("");
    setShowSecretModal(false);
  };
  const reauthorizeOneTimeSecret = (secretId: string) =>
    reauthorizeOneTimeSecretAction(
      {
        sessionId,
        agentAPIRef,
        setError,
        setShowSecretModal,
        secretLifetime,
        setIsRegisteringSecret,
        setSecretFeedback,
      },
      secretId,
    );
  return {
    showSecretModal,
    setShowSecretModal,
    secretValue,
    setSecretValue,
    secretLifetime,
    setSecretLifetime,
    isRegisteringSecret,
    setIsRegisteringSecret,
    sessionSecrets,
    setSessionSecrets,
    isLoadingSessionSecrets,
    setIsLoadingSessionSecrets,
    secretFeedback,
    setSecretFeedback,
    registerOneTimeSecret,
    loadSessionSecrets,
    closeSecretModal,
    reauthorizeOneTimeSecret,
  };
}
