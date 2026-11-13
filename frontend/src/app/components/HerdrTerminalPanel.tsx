'use client';

import { useCallback, useEffect, useState } from 'react';
import type { AgentAPIProxyClient, HerdrTerminal } from '../../lib/agentapi-proxy-client';

interface Props {
  client: AgentAPIProxyClient;
  sessionId: string;
  onClose: () => void;
}

export default function HerdrTerminalPanel({ client, sessionId, onClose }: Props) {
  const [terminals, setTerminals] = useState<HerdrTerminal[]>([]);
  const [selected, setSelected] = useState('');
  const [output, setOutput] = useState('');
  const [prompt, setPrompt] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const next = await client.listHerdrTerminals(sessionId);
      setTerminals(next);
      setSelected(current => next.some(terminal => terminal.address === current) ? current : (next[0]?.address ?? ''));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'herdr-core への接続に失敗しました');
    } finally {
      setLoading(false);
    }
  }, [client, sessionId]);

  const refreshOutput = useCallback(async () => {
    if (!selected) {
      setOutput('');
      return;
    }
    try {
      setOutput(await client.readHerdrTerminal(sessionId, selected));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'ターミナルを読めませんでした');
    }
  }, [client, selected, sessionId]);

  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => { void refreshOutput(); }, [refreshOutput]);
  useEffect(() => {
    const timer = window.setInterval(() => { void refresh(); void refreshOutput(); }, 3000);
    return () => window.clearInterval(timer);
  }, [refresh, refreshOutput]);

  const send = async () => {
    const text = prompt.trim();
    if (!selected || !text) return;
    try {
      await client.sendHerdrTerminal(sessionId, selected, text);
      setPrompt('');
      await refreshOutput();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'プロンプトを送信できませんでした');
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-modal="true" aria-label="herdr-core terminals">
      <div className="flex h-[80vh] w-full max-w-5xl flex-col overflow-hidden rounded-lg bg-white shadow-xl dark:bg-gray-900">
        <div className="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700">
          <div>
            <h2 className="font-semibold text-gray-900 dark:text-white">herdr-core ターミナル</h2>
            <p className="text-xs text-gray-500">管理中のエージェントを読み、プロンプトを送信できます</p>
          </div>
          <button type="button" onClick={onClose} className="rounded px-3 py-1 text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800">閉じる</button>
        </div>
        <div className="flex min-h-0 flex-1 flex-col sm:flex-row">
          <aside className="w-full border-b border-gray-200 p-2 sm:w-64 sm:border-b-0 sm:border-r dark:border-gray-700">
            {loading && <p className="p-2 text-sm text-gray-500">読み込み中...</p>}
            {!loading && terminals.length === 0 && <p className="p-2 text-sm text-gray-500">実行中のターミナルはありません</p>}
            {terminals.map(terminal => (
              <button key={terminal.address} type="button" onClick={() => setSelected(terminal.address)} className={`mb-1 w-full rounded p-2 text-left text-sm ${selected === terminal.address ? 'bg-blue-100 text-blue-900 dark:bg-blue-900/40 dark:text-blue-100' : 'hover:bg-gray-100 dark:hover:bg-gray-800'}`}>
                <span className="block font-mono">{terminal.name || terminal.address}</span>
                <span className="block truncate text-xs opacity-70">{terminal.status?.state || terminal.state}{terminal.status?.title ? ` · ${terminal.status.title}` : ''}</span>
              </button>
            ))}
          </aside>
          <section className="flex min-h-0 flex-1 flex-col p-3">
            {error && <p className="mb-2 rounded bg-red-50 p-2 text-sm text-red-700 dark:bg-red-950/40 dark:text-red-300">{error}</p>}
            <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap rounded bg-gray-950 p-3 font-mono text-xs text-gray-100">{output || (selected ? '出力はありません' : 'ターミナルを選択してください')}</pre>
            <div className="mt-3 flex gap-2">
              <textarea value={prompt} onChange={event => setPrompt(event.target.value)} onKeyDown={event => { if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) { event.preventDefault(); void send(); } }} disabled={!selected} rows={2} placeholder="プロンプトを入力（Ctrl/Cmd+Enter で送信）" className="min-w-0 flex-1 rounded border border-gray-300 bg-white p-2 text-sm dark:border-gray-700 dark:bg-gray-800 dark:text-white" />
              <button type="button" onClick={() => void send()} disabled={!selected || !prompt.trim()} className="rounded bg-blue-600 px-4 text-sm text-white disabled:bg-gray-400">送信</button>
              <button type="button" onClick={() => selected && void client.interruptHerdrTerminal(sessionId, selected)} disabled={!selected} className="rounded bg-red-600 px-3 text-sm text-white disabled:bg-gray-400">中断</button>
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}
