'use client'

import { useState } from 'react'
import { AgentAPIProxyClient } from '../../lib/agentapi-proxy-client'
import { SandboxDomainsDialogState, useSandboxDomainsDialog } from './useSandboxDomainsDialog'

interface SandboxDomainsButtonProps { sessionId: string; agentAPI: AgentAPIProxyClient }

export default function SandboxDomainsButton({ sessionId, agentAPI }: SandboxDomainsButtonProps) {
  const [isOpen, setIsOpen] = useState(false)
  const state = useSandboxDomainsDialog(sessionId, agentAPI, isOpen)
  return <>
    <button onClick={() => setIsOpen(true)} className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors" title="サンドボックス アクセスドメイン">
      <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" /></svg>
    </button>
    {isOpen && <SandboxDomainsModal state={state} close={() => setIsOpen(false)} />}
  </>
}

function SandboxDomainsModal({ state, close }: { state: SandboxDomainsDialogState; close: () => void }) {
  return <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4" onClick={(event) => { if (event.target === event.currentTarget) close() }}>
    <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg max-h-[80vh] flex flex-col">
      <ModalHeader close={close} />
      <ModalBody state={state} />
      <div className="px-6 py-3 border-t border-gray-200 dark:border-gray-700 shrink-0 flex justify-between items-center">
        <button onClick={state.fetchDomains} disabled={state.isLoading} className="text-sm text-blue-600 hover:text-blue-700 dark:text-blue-400 disabled:opacity-50">再読み込み</button>
        <button onClick={close} className="px-4 py-2 text-sm bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 text-gray-700 dark:text-gray-300 rounded-md transition-colors">閉じる</button>
      </div>
    </div>
  </div>
}

function ModalHeader({ close }: { close: () => void }) {
  return <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between shrink-0">
    <h2 className="text-lg font-semibold text-gray-900 dark:text-white">サンドボックス アクセスドメイン</h2>
    <button onClick={close} className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"><svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" /></svg></button>
  </div>
}

function ModalBody({ state }: { state: SandboxDomainsDialogState }) {
  return <div className="flex-1 overflow-y-auto px-6 py-4 space-y-4">
    {state.isLoading && <LoadingIndicator />}
    {state.error && !state.isLoading && <Notice tone="red">{state.error}</Notice>}
    {state.addSuccess && <Notice tone="green">{state.addSuccess}</Notice>}
    {!state.isLoading && !state.error && <>
      <DomainTabs state={state} />
      <DomainList state={state} />
      <PolicySection state={state} />
    </>}
  </div>
}

function LoadingIndicator() {
  return <div className="flex items-center justify-center py-8"><svg className="animate-spin w-6 h-6 text-blue-500" fill="none" viewBox="0 0 24 24"><circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" /><path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z" /></svg></div>
}

function Notice({ tone, children }: { tone: 'red' | 'green'; children: React.ReactNode }) {
  const style = tone === 'red' ? 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800 text-red-700 dark:text-red-400' : 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800 text-green-700 dark:text-green-400'
  return <div className={`p-3 border rounded-md text-sm ${style}`}>{children}</div>
}

function DomainTabs({ state }: { state: SandboxDomainsDialogState }) {
  return <div className="flex border-b border-gray-200 dark:border-gray-700">
    <Tab active={state.activeTab === 'denied'} tone="red" onClick={() => state.setActiveTab('denied')}>ブロック済み ({state.denied.length})</Tab>
    <Tab active={state.activeTab === 'allowed'} tone="green" onClick={() => state.setActiveTab('allowed')}>許可済み ({state.allowed.length})</Tab>
  </div>
}

function Tab({ active, tone, onClick, children }: { active: boolean; tone: 'red' | 'green'; onClick: () => void; children: React.ReactNode }) {
  const activeStyle = tone === 'red' ? 'border-red-500 text-red-600 dark:text-red-400' : 'border-green-500 text-green-600 dark:text-green-400'
  return <button onClick={onClick} className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${active ? activeStyle : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400'}`}>{children}</button>
}

function DomainList({ state }: { state: SandboxDomainsDialogState }) {
  if (state.currentDomains.length === 0) return <p className="text-sm text-gray-500 dark:text-gray-400 py-4 text-center">{state.activeTab === 'denied' ? 'ブロックされたドメインはありません' : '許可されたドメインはありません'}</p>
  const text = state.activeTab === 'denied' ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'
  return <div className="space-y-1 max-h-48 overflow-y-auto">{state.currentDomains.map((domain) =>
    <label key={domain} className="flex items-center space-x-3 px-2 py-1.5 rounded hover:bg-gray-50 dark:hover:bg-gray-700 cursor-pointer">
      <input type="checkbox" checked={state.selectedDomains.has(domain)} onChange={() => state.toggleDomain(domain)} className="rounded border-gray-300 dark:border-gray-600 text-blue-600" />
      <span className={`text-sm font-mono flex-1 truncate ${text}`}>{domain}</span>
    </label>)}</div>
}

function PolicySection({ state }: { state: SandboxDomainsDialogState }) {
  if (state.selectedDomains.size === 0) return null
  if (state.policies.length === 0) return <div className="border-t border-gray-200 dark:border-gray-700 pt-4"><p className="text-sm text-gray-500 dark:text-gray-400">ドメインを追加するには、まず<a href="/sandbox-policies" className="text-blue-600 hover:underline ml-1">サンドボックスポリシー</a>を作成してください。</p></div>
  return <div className="border-t border-gray-200 dark:border-gray-700 pt-4 space-y-3">
    <p className="text-sm font-medium text-gray-700 dark:text-gray-300">{state.selectedDomains.size} 件のドメインをポリシーに追加</p>
    <select value={state.selectedPolicyId} onChange={(event) => state.setSelectedPolicyId(event.target.value)} className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white">{state.policies.map((policy) => <option key={policy.id} value={policy.id}>{policy.name}</option>)}</select>
    <button onClick={state.addToPolicy} disabled={state.isAdding} className="w-full px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white rounded-md transition-colors disabled:cursor-not-allowed text-sm">{state.isAdding ? '追加中...' : 'ポリシーに追加（許可リスト更新）'}</button>
  </div>
}
