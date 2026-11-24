import { describe, expect, it } from 'vitest';
import { getToolBriefInfo, parseToolResultContent, parseToolUseContent, visibleToolInput } from '../messageContent';

describe('message content interpretation', () => {
  it('treats plain or malformed content as a non-tool message', () => {
    expect(parseToolUseContent('ordinary text')).toBeNull();
    expect(parseToolUseContent('{"type":"other"}')).toBeNull();
    expect(parseToolUseContent('null')).toBeNull();
    expect(parseToolResultContent('not json')).toBeNull();
  });
  it('prefers specific input over generic ACP tool titles', () => {
    const tool = {
      type: 'tool_use' as const,
      id: 'tool-1',
      name: 'Read',
      title: 'Read File',
      input: { file_path: '/repo/src/app.ts' },
    };
    expect(getToolBriefInfo(tool, null, undefined)).toBe('/repo/src/app.ts');
    expect(getToolBriefInfo({ ...tool, input: {} }, { result: '\n first result\nsecond' }, undefined)).toBe(
      'first result'
    );
  });
  it('uses descriptions first, and preserves suffixes of long paths', () => {
    const tool = {
      type: 'tool_use' as const,
      id: 'tool-1',
      name: 'Read',
      input: { description: 'description', file_path: 'path' },
    };
    expect(getToolBriefInfo(tool, null, undefined)).toBe('description');
    const path = '/very/long/'.repeat(10) + 'app.ts';
    expect(getToolBriefInfo({ ...tool, input: { file_path: path } }, null, undefined)).toBe('...' + path.slice(-40));
  });
  it('hides transport tool metadata without dropping real input', () => {
    expect(visibleToolInput({ _toolName: 'execute', command: 'ls', timeout: 10 })).toEqual({
      command: 'ls',
      timeout: 10,
    });
  });
});
