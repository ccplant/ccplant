import type { FormEvent } from 'react'
import type { Session } from '../../types/agentapi'
import type { SessionListActionContext } from './sessionListActionContext'

export async function deleteSessionAction(context: SessionListActionContext, sessionId: string): Promise<void> {
  const { agentAPI, acpClient, acpMode, setDeletingSession, setError, setSuccess, fetchSessions, onSessionsUpdate } =
    context

  if (!confirm('このセッションを削除してもよろしいですか？')) return

  try {
    setDeletingSession(sessionId)
    setError(null)
    setSuccess(null)

    if (acpMode) {
      await acpClient.closeSession(sessionId)
    } else {
      await agentAPI.delete!(sessionId)
    }

    fetchSessions()
    onSessionsUpdate()

    setSuccess('セッションを削除しました')
    setTimeout(() => setSuccess(null), 3000)
  } catch (err) {
    console.error('Failed to delete session:', err)
    setError('セッションの削除に失敗しました')
  } finally {
    setDeletingSession(null)
  }
}

export async function suspendSessionAction(context: SessionListActionContext, sessionId: string): Promise<void> {
  const { agentAPI, setSuspendingSession, setError, setSuccess, setSessions, fetchSessions } = context

  if (!confirm('このセッションをサスペンドしますか？保存後、チャットを開くと再開できます。')) return

  try {
    setSuspendingSession(sessionId)
    setError(null)
    setSuccess(null)
    const result = await agentAPI.suspendSession(sessionId)
    setSessions((current) =>
      current.map((session) =>
        session.session_id === sessionId ? { ...session, status: result.status as Session['status'] } : session
      )
    )
    setSuccess('セッションのサスペンドを開始しました')
    setTimeout(() => setSuccess(null), 3000)
    void fetchSessions(true)
  } catch (err) {
    console.error('Failed to suspend session:', err)
    setError('セッションのサスペンドに失敗しました')
  } finally {
    setSuspendingSession(null)
  }
}

export async function restartSessionWithSettingsAction(
  context: SessionListActionContext,
  sessionId: string
): Promise<void> {
  const { agentAPI, setSuspendingSession, setError, setSuccess, fetchSessions } = context

  try {
    setSuspendingSession(sessionId)
    setError(null)
    await agentAPI.restartSession(sessionId)
    setSuccess('設定を再読み込みしています。現在の処理が終了してから再起動します。')
    for (let attempt = 0; attempt < 330; attempt++) {
      await new Promise((resolve) => setTimeout(resolve, 2000))
      const operation = await agentAPI.restartStatus(sessionId)
      if (operation.phase === 'failed' || operation.error) throw new Error(operation.error || '再起動に失敗しました')
      if (operation.phase === 'ready') {
        setSuccess('設定を再読み込みし、同じ会話を再開しました')
        void fetchSessions(true)
        return
      }
    }
    setError('処理の完了待ちがタイムアウトしました。進行状況を確認してください。')
  } catch (err) {
    setError(err instanceof Error ? err.message : 'セッション操作に失敗しました')
  } finally {
    setSuspendingSession(null)
  }
}

export async function templateizeSessionAction(
  context: SessionListActionContext,
  event: FormEvent<HTMLFormElement>
): Promise<void> {
  const {
    agentAPI,
    templateTarget,
    templateName,
    templateDescription,
    setSuspendingSession,
    setError,
    setSuccess,
    setSessions,
    setTemplateTarget,
  } = context

  event.preventDefault()
  if (!templateTarget || !templateName.trim()) return
  try {
    setSuspendingSession(templateTarget.session_id)
    setError(null)
    await agentAPI.saveSessionAsWorkspace(templateTarget.session_id, templateName.trim(), templateDescription.trim())
    setSessions((current) => current.filter((item) => item.session_id !== templateTarget.session_id))
    setTemplateTarget(null)
    setSuccess('ワークスペースとして保存しました')
    setTimeout(() => setSuccess(null), 3000)
  } catch (err) {
    setError(err instanceof Error ? err.message : 'ワークスペースの保存に失敗しました')
  } finally {
    setSuspendingSession(null)
  }
}

export async function deleteSelectedSessionsAction(context: SessionListActionContext): Promise<void> {
  const {
    agentAPI,
    acpClient,
    acpMode,
    selectedSessions,
    setError,
    setSuccess,
    setIsBulkDeleting,
    setSelectedSessions,
    fetchSessions,
    onSessionsUpdate,
  } = context

  const count = selectedSessions.size
  if (count === 0) return
  if (!confirm(`${count}件のセッションを削除してもよろしいですか？`)) return

  try {
    setIsBulkDeleting(true)
    setError(null)
    setSuccess(null)

    if (acpMode) {
      await Promise.all(Array.from(selectedSessions).map((id) => acpClient.closeSession(id)))
    } else {
      await agentAPI.deleteBatch!(Array.from(selectedSessions))
    }

    setSelectedSessions(new Set())
    fetchSessions()
    onSessionsUpdate()

    setSuccess(`${count}件のセッションを削除しました`)
    setTimeout(() => setSuccess(null), 3000)
  } catch (err) {
    console.error('Failed to bulk delete sessions:', err)
    setError('セッションの一括削除に失敗しました')
  } finally {
    setIsBulkDeleting(false)
  }
}
