'use client';

import { useCallback, useEffect, useState } from 'react';
import type { AgentAPIProxyClient, HerdrTerminal } from '../../lib/agentapi-proxy-client';

interface Props {
  client: AgentAPIProxyClient;
  sessionId: string;
  selectedTerminal: string;
  onSelectTerminal: (address: string) => void;
  refreshToken?: number;
}

export default function HerdrTerminalPanel({ client, sessionId, selectedTerminal, onSelectTerminal, refreshToken = 0 }: Props) {
  const [terminals, setTerminals] = useState<HerdrTerminal[]>([]);
  const [output, setOutput] = useState('');
  const [error, setError] = useState<string | null>(null);

  const refreshTerminals = useCallback(async () => {
    try {
      const next = await client.listHerdrTerminals(sessionId);
      setTerminals(next);
      if (selectedTerminal && !next.some(terminal => terminal.address === selectedTerminal)) onSelectTerminal('');
      setError(null);
    } catch {
      setTerminals([]);
      if (selectedTerminal) onSelectTerminal('');
    }
  }, [client, onSelectTerminal, selectedTerminal, sessionId]);

  const refreshOutput = useCallback(async () => {
    if (!selectedTerminal) {
      setOutput('');
      return;
    }
    try {
      setOutput(await client.readHerdrTerminal(sessionId, selectedTerminal));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'エージェントの出力を取得できませんでした');
    }
  }, [client, selectedTerminal, sessionId]);

  useEffect(() => { void refreshTerminals(); }, [refreshTerminals]);
  useEffect(() => { void refreshOutput(); }, [refreshOutput, refreshToken]);
  useEffect(() => {
    const timer = window.setInterval(() => { void refreshTerminals(); void refreshOutput(); }, 3000);
    return () => window.clearInterval(timer);
  }, [refreshOutput, refreshTerminals]);

  if (terminals.length === 0) return null;

  return (
    <div className="mb-2 overflow-hidden rounded-md border border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-800/60">
      <div className="flex items-center gap-1.5 overflow-x-auto px-2 py-1.5" aria-label="Herdr エージェント">
        <span className="shrink-0 text-[10px] font-semibold uppercase tracking-wide text-gray-400">Herdr</span>
        {terminals.map(terminal => {
          const selected = selectedTerminal === terminal.address;
          const state = terminal.status?.state || terminal.state;
          return <button key={terminal.address} type="button" onClick={() => onSelectTerminal(selected ? '' : terminal.address)} aria-pressed={selected} title={`${terminal.name || terminal.address}${state ? ` · ${state}` : ''}`} className={`flex shrink-0 items-center gap-1.5 rounded-full border px-2 py-1 text-xs transition-colors ${selected ? 'border-blue-500 bg-blue-600 text-white' : 'border-gray-300 bg-white text-gray-700 hover:border-blue-400 hover:text-blue-700 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-200'}`}>
            <span className={`h-1.5 w-1.5 rounded-full ${state === 'running' ? 'bg-emerald-400' : 'bg-gray-400'}`} aria-hidden="true" />
            <span className="max-w-40 truncate font-mono">{terminal.name || terminal.address}</span>
          </button>;
        })}
      </div>
      {selectedTerminal && <div className="border-t border-gray-200 dark:border-gray-700">
        {error && <p className="px-3 py-1.5 text-xs text-red-600 dark:text-red-300">{error}</p>}
        <pre className="max-h-56 min-h-16 overflow-auto whitespace-pre-wrap bg-gray-950 px-3 py-2 font-mono text-xs leading-relaxed text-gray-100">{output || '出力はありません'}</pre>
      </div>}
    </div>
  );
}
