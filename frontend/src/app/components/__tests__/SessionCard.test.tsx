import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import SessionCard from '../SessionCard';

describe('SessionCard annotations', () => {
  it('expands the full running task when its preview is clicked', () => {
    const runningTask = '長い進捗の全文をクリック操作で表示するためのテスト文字列です';

    render(
      <SessionCard
        session={{
          session_id: '12345678-1234-1234-1234-123456789abc',
          user_id: 'user-1',
          status: 'active',
          started_at: '2026-07-25T00:00:00Z',
          annotations: {
            description: '長いアノテーションでもカード幅を崩さないセッション',
            running_task: runningTask,
          },
        }}
      />,
    );

    const progress = screen.getByRole('button', { name: `進捗を全文表示: ${runningTask}` });
    expect(progress).toHaveAttribute('aria-expanded', 'false');

    fireEvent.click(progress);

    expect(screen.getByRole('button', { name: `進捗を閉じる: ${runningTask}` })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText(runningTask)).toBeInTheDocument();
  });
});
