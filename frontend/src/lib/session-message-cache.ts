import type { SessionMessage } from '../types/agentapi';

const CACHE_PREFIX = 'ccplant:session-messages:';
const CACHE_TTL_MS = 30 * 60 * 1000;
const MAX_CACHE_BYTES = 1024 * 1024;

interface CachedSessionMessages {
  savedAt: number;
  messages: SessionMessage[];
}

function storage(): Storage | null {
  if (typeof window === 'undefined') return null;
  try {
    return window.sessionStorage;
  } catch {
    return null;
  }
}

export function readCachedSessionMessages(sessionId: string): SessionMessage[] | null {
  const target = storage();
  if (!target) return null;

  const key = `${CACHE_PREFIX}${sessionId}`;
  try {
    const raw = target.getItem(key);
    if (!raw) return null;
    const cached = JSON.parse(raw) as CachedSessionMessages;
    if (!Array.isArray(cached.messages) || Date.now() - cached.savedAt > CACHE_TTL_MS) {
      target.removeItem(key);
      return null;
    }
    return cached.messages;
  } catch {
    target.removeItem(key);
    return null;
  }
}

export function writeCachedSessionMessages(sessionId: string, messages: SessionMessage[]): void {
  const target = storage();
  if (!target || messages.length === 0) return;

  try {
    const value = JSON.stringify({ savedAt: Date.now(), messages } satisfies CachedSessionMessages);
    if (new Blob([value]).size > MAX_CACHE_BYTES) return;
    target.setItem(`${CACHE_PREFIX}${sessionId}`, value);
  } catch {
    // Cache writes are best effort and must never interfere with chat rendering.
  }
}
