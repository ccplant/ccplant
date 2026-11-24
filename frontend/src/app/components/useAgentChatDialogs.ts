import { useState } from "react";
import type { PendingAction } from "../../types/agentapi";
import { useEscapeModalDismissal } from "./useAgentChatUIEffects";

export function useAgentChatDialogs() {
  const [showControlPanel, setShowControlPanel] = useState(false);
  const [showTemplates, setShowTemplates] = useState(false);
  const [showTemplateModal, setShowTemplateModal] = useState(false);
  const [showComposerMenu, setShowComposerMenu] = useState(false);
  const [showPRLinks, setShowPRLinks] = useState(false);
  const [showFontSettings, setShowFontSettings] = useState(false);
  const [showSessionInfo, setShowSessionInfo] = useState(false);
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null);
  const [showQuestionModal, setShowQuestionModal] = useState(false);
  useEscapeModalDismissal({
    showQuestionModal,
    setShowQuestionModal,
    showTemplateModal,
    setShowTemplateModal,
    showPRLinks,
    setShowPRLinks,
    showFontSettings,
    setShowFontSettings,
    showComposerMenu,
    setShowComposerMenu,
  });
  return {
    showControlPanel,
    setShowControlPanel,
    showTemplates,
    setShowTemplates,
    showTemplateModal,
    setShowTemplateModal,
    showComposerMenu,
    setShowComposerMenu,
    showPRLinks,
    setShowPRLinks,
    showFontSettings,
    setShowFontSettings,
    showSessionInfo,
    setShowSessionInfo,
    pendingAction,
    setPendingAction,
    showQuestionModal,
    setShowQuestionModal,
  };
}
