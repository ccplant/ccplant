'use client';

import { useCallback, useEffect, useState } from 'react';
import { MessageTemplate } from '../../types/messageTemplate';
import { messageTemplateManager } from '../../utils/messageTemplateManager';
import { recentMessagesManager } from '../../utils/recentMessagesManager';

export function useChatMessageLibrary() {
  const [recentMessages, setRecentMessages] = useState<string[]>([]);
  const [templates, setTemplates] = useState<MessageTemplate[]>([]);
  const loadTemplates = useCallback(async () => {
    try {
      const allTemplates = await messageTemplateManager.getTemplates();
      setTemplates(allTemplates);
    } catch (error) {
      console.error('Failed to load templates:', error);
      setTemplates([]);
    }
  }, []);

  const loadRecentMessages = useCallback(async () => {
    try {
      const messages = await recentMessagesManager.getRecentMessages();
      setRecentMessages(messages.map((msg) => msg.content));
    } catch (error) {
      console.error('Failed to load recent messages:', error);
    }
  }, []);

  useEffect(() => {
    // Defer template and recent message loading to improve initial render speed
    const timeoutId = setTimeout(() => {
      loadTemplates();
      loadRecentMessages();
    }, 100);
    return () => clearTimeout(timeoutId);
  }, [loadRecentMessages, loadTemplates]);

  return { templates, setTemplates, recentMessages, setRecentMessages, loadTemplates, loadRecentMessages };
}
