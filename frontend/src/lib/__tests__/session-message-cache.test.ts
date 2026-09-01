import { afterEach, describe, expect, it, vi } from 'vitest';
import { readCachedSessionMessages, writeCachedSessionMessages } from '../session-message-cache';

describe('session message cache', () => {
  afterEach(() => {
    sessionStorage.clear();
    vi.useRealTimers();
  });

  it('restores messages saved for the current tab', () => {
    const messages = [{ id: 1, role: 'agent' as const, content: 'ready', time: 'now', type: 'normal' as const }];
    writeCachedSessionMessages('session-1', messages);
    expect(readCachedSessionMessages('session-1')).toEqual(messages);
  });

  it('drops entries after the short-lived cache expires', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-09-01T00:00:00Z'));
    writeCachedSessionMessages('session-1', [{ id: 1, role: 'agent', content: 'old', time: 'now', type: 'normal' }]);
    vi.advanceTimersByTime(31 * 60 * 1000);
    expect(readCachedSessionMessages('session-1')).toBeNull();
  });
});
