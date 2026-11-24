import { useCallback, useEffect, useState } from 'react';
import { AgentAPIProxyClient, AgentAPIProxyError } from '../../lib/agentapi-proxy-client';
import { ShareStatus } from '../../types/share';

export function useShareSession(sessionId: string, agentAPI: AgentAPIProxyClient) {
  const [isLoading, setIsLoading] = useState(false);
  const [shareStatus, setShareStatus] = useState<ShareStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const fetchShareStatus = useCallback(async () => {
    try { setShareStatus(await agentAPI.getShareStatus(sessionId)); }
    catch (err) { console.error('Failed to fetch share status:', err); }
  }, [agentAPI, sessionId]);
  useEffect(() => { void fetchShareStatus(); }, [fetchShareStatus]);

  const createShare = async () => {
    setIsLoading(true); setError(null);
    try {
      const result = await agentAPI.createShare(sessionId);
      setShareStatus({ token: result.token, session_id: result.session_id, share_url: result.share_url, created_by: '', created_at: result.created_at, expires_at: result.expires_at, is_expired: false });
    } catch (err) {
      console.error('Failed to create share:', err);
      setError(err instanceof AgentAPIProxyError ? `共有リンクの作成に失敗しました: ${err.message}` : '共有リンクの作成に失敗しました');
    } finally { setIsLoading(false); }
  };
  const revokeShare = async () => {
    if (!confirm('共有リンクを取り消しますか？この操作を行うと、共有URLは無効になります。')) return;
    setIsLoading(true); setError(null);
    try { await agentAPI.revokeShare(sessionId); setShareStatus(null); }
    catch (err) {
      console.error('Failed to revoke share:', err);
      setError(err instanceof AgentAPIProxyError ? `共有の取り消しに失敗しました: ${err.message}` : '共有の取り消しに失敗しました');
    } finally { setIsLoading(false); }
  };
  const copyUrl = async () => {
    if (!shareStatus) return;
    try {
      await navigator.clipboard.writeText(`${window.location.origin}${shareStatus.share_url}`);
      setCopied(true); setTimeout(() => setCopied(false), 2000);
    } catch (err) { console.error('Failed to copy URL:', err); setError('URLのコピーに失敗しました'); }
  };
  return { isLoading, shareStatus, error, copied, createShare, revokeShare, copyUrl };
}
