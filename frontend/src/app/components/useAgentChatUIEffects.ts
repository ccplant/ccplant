import { useEffect, type Dispatch, type MutableRefObject, type RefObject, type SetStateAction } from 'react';
import type { SessionMessage } from '../../types/agentapi';
import type { FontSettings } from '../../types/settings';

interface ModalDismissalState {
  showQuestionModal: boolean;
  setShowQuestionModal: Dispatch<SetStateAction<boolean>>;
  showTemplateModal: boolean;
  setShowTemplateModal: Dispatch<SetStateAction<boolean>>;
  showPRLinks: boolean;
  setShowPRLinks: Dispatch<SetStateAction<boolean>>;
  showFontSettings: boolean;
  setShowFontSettings: Dispatch<SetStateAction<boolean>>;
  showComposerMenu: boolean;
  setShowComposerMenu: Dispatch<SetStateAction<boolean>>;
}

export function useEscapeModalDismissal(state: ModalDismissalState) {
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      if (state.showQuestionModal) state.setShowQuestionModal(false);
      else if (state.showTemplateModal) state.setShowTemplateModal(false);
      else if (state.showPRLinks) state.setShowPRLinks(false);
      else if (state.showFontSettings) state.setShowFontSettings(false);
      else if (state.showComposerMenu) state.setShowComposerMenu(false);
    };
    const isAnyModalOpen =
      state.showQuestionModal ||
      state.showTemplateModal ||
      state.showPRLinks ||
      state.showFontSettings ||
      state.showComposerMenu;
    if (isAnyModalOpen) document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [state]);
}

export function useFontSettingsListener(setFontSettings: Dispatch<SetStateAction<FontSettings>>) {
  useEffect(() => {
    const handleChange = (event: Event) => {
      const settings = (event as CustomEvent<FontSettings>).detail;
      if (settings) setFontSettings(settings);
    };
    window.addEventListener('fontSettingsChanged', handleChange);
    return () => window.removeEventListener('fontSettingsChanged', handleChange);
  }, [setFontSettings]);
}

interface MessageAutoScrollState {
  messages: SessionMessage[];
  shouldAutoScroll: boolean;
  messagesEndRef: RefObject<HTMLDivElement | null>;
  previousLengthRef: MutableRefObject<number>;
  setHasNewMessages: Dispatch<SetStateAction<boolean>>;
}

export function useMessageAutoScroll(state: MessageAutoScrollState) {
  useEffect(() => {
    if (state.messages.length > state.previousLengthRef.current) {
      if (state.shouldAutoScroll) state.messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
      else state.setHasNewMessages(true);
    }
    state.previousLengthRef.current = state.messages.length;
  }, [state]);
}

interface HistoryIntersectionState {
  topRef: RefObject<HTMLDivElement | null>;
  containerRef: RefObject<HTMLDivElement | null>;
  hasMoreMessages: boolean;
  isInitialLoadComplete: boolean;
  isLoadingMore: boolean;
  loadMoreMessages: () => Promise<void>;
}

export function useHistoryIntersection(state: HistoryIntersectionState) {
  useEffect(() => {
    const topElement = state.topRef.current;
    if (!topElement || !state.hasMoreMessages || !state.isInitialLoadComplete || state.isLoadingMore) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) void state.loadMoreMessages();
      },
      { threshold: 0.1, root: state.containerRef.current },
    );
    observer.observe(topElement);
    return () => observer.disconnect();
  }, [state]);
}
