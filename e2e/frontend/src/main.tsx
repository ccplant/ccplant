import React, { useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { deleteCredential, listCredentials, loadCredential, saveCredential, type AuthType, type SavedCredential } from './credentialStore';
import './styles.css';

type Category = { id: string; description: string };
type Scenario = { id: string; category: string; description: string; kind: string };
type Run = { id: string; scenario_id: string; scenario_description: string; category_id: string; category_description: string; kind: string; environment: string; status: string; created_at: string };
type Event = { sequence: number; type: string; message?: string; test?: string; created_at: string };
const API = import.meta.env.VITE_E2E_API_URL || 'http://localhost:8081';

function App() {
  const [categories, setCategories] = useState<Category[]>([]), [scenarios, setScenarios] = useState<Scenario[]>([]), [runs, setRuns] = useState<Run[]>([]);
  const [category, setCategory] = useState('all'), [selected, setSelected] = useState<string>(), [events, setEvents] = useState<Event[]>([]);
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('');
  const [apiURL, setAPIURL] = useState('http://localhost:8080'), [frontendURL, setFrontendURL] = useState('http://localhost:3000');
  const [authType, setAuthType] = useState<'none' | AuthType>('none'), [authValue, setAuthValue] = useState('');
  const [credentialName, setCredentialName] = useState(''), [savedCredentials, setSavedCredentials] = useState<SavedCredential[]>([]);

  const refreshCredentials = async () => setSavedCredentials(await listCredentials());
  const refresh = async () => {
    try {
      const [catalog, history] = await Promise.all([fetch(`${API}/api/v1/scenarios`).then(r => r.json()), fetch(`${API}/api/v1/runs`).then(r => r.json())]);
      setCategories(catalog.categories); setScenarios(catalog.scenarios); setRuns(history.runs);
      if (!selected && history.runs[0]) setSelected(history.runs[0].id);
    } catch (cause) { setError(String(cause)); }
  };
  useEffect(() => {
    refresh(); refreshCredentials().catch(cause => setError(`認証情報を読み込めません: ${String(cause)}`));
    const interval = setInterval(refresh, 2000); return () => clearInterval(interval);
  }, []);
  useEffect(() => {
    if (!selected) { setEvents([]); return; }
    fetch(`${API}/api/v1/runs/${selected}`).then(r => r.json()).then(d => setEvents(d.events || [])).catch(cause => setError(String(cause)));
  }, [selected, runs]);
  const visible = useMemo(() => category === 'all' ? runs : runs.filter(r => r.category_id === category), [runs, category]);
  const chosen = runs.find(r => r.id === selected);

  const start = async (id: string) => {
    setBusy(true); setError(''); setNotice('');
    try {
      const response = await fetch(`${API}/api/v1/runs`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ scenario_id: id, environment: 'custom', parameters: {}, target: { api_url: apiURL, frontend_url: frontendURL, auth_type: authType, auth_value: authValue } }) });
      if (!response.ok) throw new Error(await response.text());
      setAuthValue(''); const run = await response.json(); setSelected(run.id); await refresh();
    } catch (cause) { setError(String(cause)); } finally { setBusy(false); }
  };
  const selectSavedCredential = async (name: string) => {
    setCredentialName(name); setNotice(''); if (!name) return;
    try { const credential = await loadCredential(name); setAuthType(credential.authType); setAuthValue(credential.value); setNotice(`「${name}」を読み込みました。`); }
    catch (cause) { setError(`認証情報を復号できません: ${String(cause)}`); }
  };
  const persistCredential = async () => {
    const name = credentialName.trim();
    if (!name || authType === 'none' || !authValue) { setError('名前・認証方式・認証情報を入力してください。'); return; }
    try { await saveCredential(name, authType, authValue); setCredentialName(name); await refreshCredentials(); setError(''); setNotice(`「${name}」を暗号化してこのブラウザに保存しました。`); }
    catch (cause) { setError(`認証情報を保存できません: ${String(cause)}`); }
  };
  const removeCredential = async () => {
    if (!credentialName || !savedCredentials.some(item => item.name === credentialName)) return;
    try { const name = credentialName; await deleteCredential(name); setCredentialName(''); setAuthValue(''); await refreshCredentials(); setError(''); setNotice(`「${name}」を削除しました。`); }
    catch (cause) { setError(`認証情報を削除できません: ${String(cause)}`); }
  };

  return <main><header><div><h1>検証管制卓</h1><p>APIと画面のシナリオを、同じ実行履歴で追跡します。</p></div><div className="pulse"><i /> worker polling</div></header>
    {error && <div className="error">{error}</div>}{notice && <div className="notice">{notice}</div>}
    <section className="workspace"><aside><h2>分類</h2><button className={category === 'all' ? 'active' : ''} onClick={() => setCategory('all')}>すべて <b>{runs.length}</b></button>
      {categories.map(c => <button key={c.id} className={category === c.id ? 'active' : ''} onClick={() => setCategory(c.id)}>{c.description}<b>{runs.filter(r => r.category_id === c.id).length}</b></button>)}
      <h2 className="launchTitle">接続先</h2><div className="target"><label>API URL<input value={apiURL} onChange={e => setAPIURL(e.target.value)} /></label><label>Frontend URL<input value={frontendURL} onChange={e => setFrontendURL(e.target.value)} /></label>
        <label>認証<select value={authType} onChange={e => setAuthType(e.target.value as 'none' | AuthType)}><option value="none">なし</option><option value="bearer">Bearer</option><option value="api-key">API key</option></select></label>
        {authType !== 'none' && <><label>保存済み<select value={savedCredentials.some(item => item.name === credentialName) ? credentialName : ''} onChange={e => selectSavedCredential(e.target.value)}><option value="">選択してください</option>{savedCredentials.map(item => <option key={item.name} value={item.name}>{item.name} ({item.authType})</option>)}</select></label>
          <label>名前<input value={credentialName} maxLength={80} placeholder="例: staging admin" onChange={e => setCredentialName(e.target.value)} /></label><label>認証情報<input type="password" autoComplete="off" value={authValue} onChange={e => setAuthValue(e.target.value)} /><small>実行時だけAPIへ渡し、サーバーには保存しません。</small></label>
          <div className="credentialActions"><button type="button" onClick={persistCredential}>暗号化して保存</button><button type="button" className="danger" disabled={!savedCredentials.some(item => item.name === credentialName)} onClick={removeCredential}>削除</button></div><p className="securityNote">AES-GCM鍵と暗号文をこのブラウザの IndexedDB に保存します。</p></>}
      </div><h2 className="launchTitle">新しい実行</h2>{scenarios.filter(s => category === 'all' || s.category === category).map(s => <button disabled={busy} className="scenario" key={s.id} onClick={() => start(s.id)}><span>{s.description}</span><small>{s.kind}</small></button>)}</aside>
      <div className="runs"><div className="sectionHead"><h2>実行履歴</h2><span>{visible.length} runs</span></div><div className="table">{visible.length === 0 ? <div className="empty">まだ実行結果がありません。左側からシナリオを開始してください。</div> : visible.map(r => <button className={selected === r.id ? 'row selected' : 'row'} key={r.id} onClick={() => setSelected(r.id)}><i className={`status ${r.status}`} /><span className="name">{r.scenario_description}<small>{r.category_description}</small></span><span className="kind">{r.kind}</span><time>{new Date(r.created_at).toLocaleString('ja-JP')}</time><strong>{r.status}</strong></button>)}</div></div>
      <aside className="detail"><h2>実行詳細</h2>{chosen ? <><div className="result"><span className={`status ${chosen.status}`} /><div><strong>{chosen.scenario_description}</strong><small>{chosen.scenario_id}</small></div></div><dl><dt>状態</dt><dd>{chosen.status}</dd><dt>分類</dt><dd>{chosen.category_description}</dd><dt>環境</dt><dd>{chosen.environment}</dd></dl><h3>タイムライン</h3><div className="events">{events.length === 0 ? <p>workerの開始を待っています。</p> : events.map(e => <div key={e.sequence}><b>{e.type}</b><span>{e.test || e.message}</span></div>)}</div></> : <p className="empty">実行を選択してください。</p>}</aside></section></main>;
}
createRoot(document.getElementById('root')!).render(<React.StrictMode><App /></React.StrictMode>);
