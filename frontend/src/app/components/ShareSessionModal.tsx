import { AgentAPIProxyClient } from '../../lib/agentapi-proxy-client';
import { ShareStatus } from '../../types/share';
import { useShareSession } from './useShareSession';

interface ShareSessionModalProps { sessionId: string; agentAPI: AgentAPIProxyClient; onClose: () => void; }

function InfoBox({ shared }: { shared: boolean }) {
  return <div className={`p-3 border rounded-md ${shared ? 'bg-purple-50 dark:bg-purple-900/20 border-purple-200 dark:border-purple-800' : 'bg-blue-50 dark:bg-blue-900/20 border-blue-200 dark:border-blue-800'}`}>
    <div className="flex items-start space-x-2">
      <svg className={`w-5 h-5 mt-0.5 ${shared ? 'text-purple-500' : 'text-blue-500'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
      <p className={`text-sm ${shared ? 'text-purple-700 dark:text-purple-300' : 'text-blue-700 dark:text-blue-300'}`}>{shared ? '共有リンクを知っているログインユーザーは、このセッションを読み取り専用で閲覧できます。' : '共有リンクを持つログイン済みユーザーのみが閲覧できます。メッセージの送信はできません。'}</p>
    </div>
  </div>;
}

function formatDate(value: string | null) { return value ? new Date(value).toLocaleString('ja-JP') : '無期限'; }
interface SharedContentProps { status: ShareStatus; copied: boolean; isLoading: boolean; onCopy: () => void; onRevoke: () => void; }
function SharedContent({ status, copied, isLoading, onCopy, onRevoke }: SharedContentProps) {
  const shareUrl = `${typeof window !== 'undefined' ? window.location.origin : ''}${status.share_url}`;
  return <div className="space-y-4">
    <div className="flex items-center space-x-2"><svg className="w-5 h-5 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" /></svg><span className="text-sm font-medium text-gray-900 dark:text-white">このセッションは共有されています</span></div>
    <div className="p-3 bg-gray-50 dark:bg-gray-700 rounded-md">
      <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">共有URL</label>
      <div className="flex items-center space-x-2"><input type="text" readOnly value={shareUrl} className="flex-1 px-2 py-1 text-sm bg-white dark:bg-gray-600 border border-gray-300 dark:border-gray-500 rounded text-gray-900 dark:text-white" /><button onClick={onCopy} className={`px-3 py-1 text-sm rounded transition-colors ${copied ? 'bg-green-600 text-white' : 'bg-blue-600 hover:bg-blue-700 text-white'}`}>{copied ? 'コピー済み' : 'コピー'}</button></div>
    </div>
    <div className="text-sm text-gray-600 dark:text-gray-400"><span className="font-medium">有効期限:</span> {formatDate(status.expires_at)}</div>
    <div className="text-sm text-gray-600 dark:text-gray-400"><span className="font-medium">作成日時:</span> {formatDate(status.created_at)}</div>
    <InfoBox shared />
    <button onClick={onRevoke} disabled={isLoading} className="w-full px-4 py-2 text-red-600 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300 border border-red-300 dark:border-red-600 hover:bg-red-50 dark:hover:bg-red-900/20 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed">{isLoading ? '処理中...' : '共有を取り消す'}</button>
  </div>;
}

function UnsharedContent({ isLoading, onCreate }: { isLoading: boolean; onCreate: () => void }) {
  return <div className="space-y-4">
    <p className="text-sm text-gray-600 dark:text-gray-400">このセッションを他のユーザーと共有できます。共有されたセッションは読み取り専用で閲覧できます。</p>
    <InfoBox shared={false} />
    <button onClick={onCreate} disabled={isLoading} className="w-full px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-300 dark:disabled:bg-gray-600 text-white rounded-md transition-colors disabled:cursor-not-allowed">{isLoading ? '作成中...' : '共有リンクを作成'}</button>
  </div>;
}

export default function ShareSessionModal({ sessionId, agentAPI, onClose }: ShareSessionModalProps) {
  const state = useShareSession(sessionId, agentAPI);
  return <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4" onClick={(event) => event.target === event.currentTarget && onClose()}>
    <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-md w-full">
      <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700"><div className="flex items-center justify-between"><h2 className="text-lg font-semibold text-gray-900 dark:text-white">セッションを共有</h2><button onClick={onClose} className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"><svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" /></svg></button></div></div>
      <div className="px-6 py-4">
        {state.error && <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-red-700 dark:text-red-400 text-sm">{state.error}</div>}
        {state.shareStatus ? <SharedContent status={state.shareStatus} copied={state.copied} isLoading={state.isLoading} onCopy={state.copyUrl} onRevoke={state.revokeShare} /> : <UnsharedContent isLoading={state.isLoading} onCreate={state.createShare} />}
      </div>
    </div>
  </div>;
}
