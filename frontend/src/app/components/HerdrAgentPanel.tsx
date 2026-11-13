'use client';

import { useEffect, useRef, useState } from 'react';
import type { AgentAPIProxyClient } from '../../lib/agentapi-proxy-client';
import type { HerdrAgent, HerdrTarget } from '../../lib/herdr-types';

function key(a: HerdrTarget) { return `${a.server_id}/${a.pane_id}/${a.generation}`; }
function target(a: HerdrTarget): HerdrTarget {
  return { server_id: a.server_id, pane_id: a.pane_id, generation: a.generation };
}

export default function HerdrAgentPanel({ sessionId, client }: { sessionId: string; client: AgentAPIProxyClient }) {
  const [open, setOpen] = useState(false);
  const [agents, setAgents] = useState<HerdrAgent[]>([]);
  const [selection, setSelection] = useState('');
  const [output, setOutput] = useState('');
  const [error, setError] = useState('');
  const [sendError, setSendError] = useState('');
  const [text, setText] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const [sending, setSending] = useState(false);
  const [loading, setLoading] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const inputLock = useRef(false);
  const current = agents.find(a => key(a) === selection);

  useEffect(() => {
    setSelection(''); setOutput(''); setText(''); setConfirmed(false); setSendError('');
  }, [sessionId]);

  useEffect(() => {
    if (!open) return;
    let live = true;
    let busy = false;
    async function update() {
      if (busy || inputLock.current || document.visibilityState === 'hidden') return;
      busy = true;
      setLoading(true);
      try {
        const response = await client.listHerdrAgents(sessionId);
        if (!live) return;
        setAgents(response.agents);
        setError(response.input_disabled ? '先の入力の配送結果が不明なため、追加入力を停止しました。runtime再起動だけでは解除しません。対象terminalまたはPod全体を確実に終了してから管理者が復旧してください。' : response.unavailable_servers ? `${response.unavailable_servers} 個のHerdrサーバーに接続できません。` : '');
        const selected = response.agents.find(a => key(a) === selection);
        if (selected) {
          const result = await client.readHerdrOutput(sessionId, target(selected));
          if (live) setOutput(result.text);
        } else {
          setOutput('');
        }
      } catch (e) {
        if (live) {
          // Never leave stale output looking like a current terminal.
          setAgents([]); setOutput('');
          setError(`Herdrを取得できません。ACP runtimeと AGENTAPI_HERDR_ENABLED=1 を確認してください。${e instanceof Error ? ` (${e.message})` : ''}`);
        }
      } finally {
        busy = false;
        if (live) setLoading(false);
      }
    }
    void update();
    const timer = window.setInterval(() => void update(), 3000);
    return () => { live = false; window.clearInterval(timer); };
  }, [open, client, sessionId, selection, refresh]);

  async function send() {
    if (!current || inputLock.current || !confirmed || !text.trim()) return;
    inputLock.current = true;
    setSending(true); setSendError('');
    try {
      await client.sendHerdrInput(sessionId, target(current), text);
      setText('');
    } catch (e) {
      setSendError(`送信結果を確認できません。出力を確認してから再操作してください。自動再送はしません。${e instanceof Error ? ` (${e.message})` : ''}`);
      setConfirmed(false);
    } finally {
      inputLock.current = false;
      setSending(false); setRefresh(n => n + 1);
    }
  }

  const ready = current?.can_input;
  return <>
    <button type="button" className="rounded-md p-2 text-sm hover:bg-gray-100 dark:hover:bg-gray-800" onClick={() => setOpen(true)}>
      Pod内エージェント
    </button>
    {open && <div className="fixed inset-0 z-50 flex justify-end bg-black/40">
      <section role="dialog" aria-modal="true" aria-label="Pod内エージェント" className="flex h-full w-full max-w-3xl flex-col gap-3 overflow-y-auto bg-white p-4 text-gray-900 shadow-xl dark:bg-gray-900 dark:text-gray-100">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-bold">Pod内エージェント</h2>
          <button type="button" disabled={sending} onClick={() => setOpen(false)}>閉じる</button>
        </div>
        <p className="text-sm">既存TUIへの接続です。会話の再開や新規エージェント起動は行いません。出力は会話履歴ではなくターミナル表示です。</p>
        <div className="flex gap-2">
          <select aria-label="エージェントを選択" className="min-w-0 flex-1 rounded border p-2 dark:bg-gray-800" value={selection} disabled={sending} onChange={e => {
            setSelection(e.target.value); setOutput(''); setText(''); setConfirmed(false); setSendError('');
          }}>
            <option value="">エージェントを選択</option>
            {agents.map(a => <option key={key(a)} value={key(a)}>{a.server_name} / {a.label || a.pane_id} — {a.agent} ({a.status})</option>)}
          </select>
          <button type="button" disabled={loading || sending} onClick={() => setRefresh(n => n + 1)}>更新</button>
        </div>
        {error && <p role="alert" className="text-sm text-red-600 dark:text-red-400">{error}</p>}
        {!loading && !error && agents.length === 0 && <p>検出されたAIエージェントはありません。未検出のプログラムは表示されません。</p>}
        {current && <p className="break-all text-xs">{current.runtime} / {current.pane_id} / {current.agent} / {current.status}{current.session_id ? ` / session: ${current.session_id}` : ''}</p>}
        {current && !current.can_input && <p className="text-sm">この対象は表示専用です。hcはidle状態の直接起動・非keepのAIのみ入力できます。通常Herdrはnative session情報も必要です。</p>}
        <pre aria-label="エージェントの出力" className="min-h-48 flex-1 overflow-auto whitespace-pre-wrap break-words rounded bg-gray-950 p-3 font-mono text-xs text-gray-100">{output || '出力なし'}</pre>
        <p className="text-xs">他の人・管理AIとの排他制御はありません。操作を調整してから送信してください。作業中・許可待ち・状態不明のエージェントには送信できません。</p>
        <label className="flex items-start gap-2 text-sm">
          <input type="checkbox" checked={confirmed} disabled={!current || sending} onChange={e => setConfirmed(e.target.checked)} />
          選択先への入力とEnter送信を許可する（TUIの状態によってはコマンドとして実行されます）
        </label>
        <textarea aria-label="エージェントへの入力" className="min-h-24 rounded border p-2 dark:bg-gray-800" value={text} disabled={!ready || sending} maxLength={16000} onChange={e => setText(e.target.value)} />
        <button type="button" className="rounded bg-blue-600 p-2 text-white disabled:opacity-40" disabled={!ready || !confirmed || !text.trim() || sending} onClick={() => void send()}>{sending ? '送信中…' : '選択したエージェントに送信'}</button>
        {sendError && <p role="alert" className="text-sm text-red-600 dark:text-red-400">{sendError}</p>}
      </section>
    </div>}
  </>;
}