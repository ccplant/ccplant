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

  const selected = terminals.find(terminal => terminal.address === selectedTerminal);
  const selectedState = selected?.status?.state || selected?.state;

  return (
    <div className="mb-2 overflow-hidden rounded-md border border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-800/60">
      <div className="flex items-center gap-2 px-2 py-1.5">
        <label htmlFor="herdr-agent-select" className="shrink-0 text-xs font-medium text-gray-500 dark:text-gray-400">Herdr</label>
        {selected && <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${selectedState === 'running' ? 'bg-emerald-400' : 'bg-gray-400'}`} aria-hidden="true" />}
        <select
          id="herdr-agent-select"
          aria-label="Herdr エージェント"
          value={selectedTerminal}
          onChange={event => onSelectTerminal(event.target.value)}
          className="min-w-0 flex-1 truncate rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-700 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-200"
        >
          <option value="">通常のチャット</option>
          {terminals.map(terminal => {
            const state = terminal.status?.state || terminal.state;
            const name = terminal.name || terminal.address;
            return <option key={terminal.address} value={terminal.address}>{state ? `${name} (${state})` : name}</option>;
          })}
        </select>
        <span className="shrink-0 text-[11px] tabular-nums text-gray-400" aria-label={`${terminals.length} エージェント`}>{terminals.length}</span>
      </div>
      {selectedTerminal && <div className="border-t border-gray-200 dark:border-gray-700">
        {error && <p className="px-3 py-1.5 text-xs text-red-600 dark:text-red-300">{error}</p>}
        <pre className="max-h-56 min-h-16 overflow-auto whitespace-pre-wrap bg-gray-950 px-3 py-2 font-mono text-xs leading-relaxed text-gray-100">{output || '出力はありません'}</pre>
      </div>}
    </div>
  );
}
