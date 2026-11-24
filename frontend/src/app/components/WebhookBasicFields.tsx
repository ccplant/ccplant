'use client'

import type { WebhookType, WebhookSignatureType } from '../../types/webhook'

import type { useWebhookForm } from './useWebhookForm'

export function WebhookBasicFields(props: ReturnType<typeof useWebhookForm>) {
  const {
    name,
    setName,
    webhookType,
    setWebhookType,
    secret,
    setSecret,
    signatureHeader,
    setSignatureHeader,
    signatureType,
    setSignatureType,
    signaturePrefix,
    setSignaturePrefix,
    isSubmitting,
    isEditing,
  } = props
  return (
    <div className="space-y-4">
      <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 border-b border-gray-200 dark:border-gray-700 pb-2">
        基本情報
      </h3>

      {/* Webhook Name */}
      <div>
        <label htmlFor="name" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          Webhook名 *
        </label>
        <input
          id="name"
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="例: PR Review Bot"
          disabled={isSubmitting}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
          required
        />
      </div>

      {/* Webhook Type */}
      <div>
        <label htmlFor="webhookType" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          Webhookタイプ *
        </label>
        <select
          id="webhookType"
          value={webhookType}
          onChange={(e) => setWebhookType(e.target.value as WebhookType)}
          disabled={isSubmitting || isEditing}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white disabled:opacity-50 disabled:cursor-not-allowed"
          required
        >
          <option value="github">GitHub</option>
          <option value="custom">Custom</option>
        </select>
        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
          {isEditing ? 'タイプは編集時に変更できません' : 'GitHubまたはカスタムwebhookを選択'}
        </p>
      </div>

      {/* Signature Header */}
      <div>
        <label htmlFor="signatureHeader" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          署名ヘッダー
        </label>
        <input
          id="signatureHeader"
          type="text"
          value={signatureHeader}
          onChange={(e) => setSignatureHeader(e.target.value)}
          placeholder="X-Signature"
          disabled={isSubmitting}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
        />
        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
          署名を含むHTTPヘッダー名（デフォルト: X-Signature）
        </p>
      </div>

      {/* Signature Type */}
      <div>
        <label htmlFor="signatureType" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          署名タイプ
        </label>
        <select
          id="signatureType"
          value={signatureType}
          onChange={(e) => setSignatureType(e.target.value as WebhookSignatureType)}
          disabled={isSubmitting}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
        >
          <option value="hmac">HMAC</option>
          <option value="static">Static Token</option>
        </select>
        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">署名検証方式（HMAC推奨）</p>
      </div>

      {/* Signature Prefix */}
      {signatureType === 'hmac' && (
        <div>
          <label htmlFor="signaturePrefix" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
            署名プレフィックス
          </label>
          <input
            id="signaturePrefix"
            type="text"
            value={signaturePrefix}
            onChange={(e) => setSignaturePrefix(e.target.value)}
            placeholder="例: sha256= (GitHub形式) / 空欄で自動検出"
            disabled={isSubmitting}
            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
          />
          <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
            署名ヘッダーの値から取り除くプレフィックス。空欄の場合は自動検出（
            <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded">sha256=hexdigest</code> 形式も{' '}
            <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded">hexdigest</code> のみの形式も対応）。Sentry など
            plain hex を送るサービスは空欄のままで動作します。
          </p>
        </div>
      )}

      {/* Secret */}
      <div>
        <label htmlFor="secret" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          シークレット（省略時は自動生成）
        </label>
        <input
          id="secret"
          type="password"
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          placeholder="外部サービスの Client Secret（例: Sentry Client Secret）"
          disabled={isSubmitting}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white"
        />
        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
          外部サービスが指定するシークレットを入力します。空の場合はシークレットが自動生成されます。
        </p>
      </div>
    </div>
  )
}
