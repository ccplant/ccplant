"use client";

import { RefreshCw } from "lucide-react";
import { SettingsPageHeader, SettingsSubsection } from "@/components/settings";
import { useSessionPoolsAdmin } from "./useSessionPoolsAdmin";
import { SessionPoolsSetup } from "./SessionPoolsSetup";
import { SessionPoolManagers } from "./SessionPoolManagers";
import { SessionLogicalPools } from "./SessionLogicalPools";

export default function SessionPoolsAdminPage() {
  const model = useSessionPoolsAdmin();
  const { runtimeStatuses, loading, connectionToken, setConnectionToken, error, reload } = model;

  return (
    <>
      <SettingsPageHeader
        title="Session Pools"
        description="Logical Pool、Manager ごとの供給設定、user / team の利用権限をクラスタ全体で管理します。"
        action={
          <button type="button" onClick={() => void reload()} disabled={loading} className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
            <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} /> 更新
          </button>
        }
      />

      {error && (
        <div role="alert" className="mb-5 rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-800 dark:bg-red-900/20">
          <p className="text-sm text-red-600 dark:text-red-400">{error}</p>
        </div>
      )}

      {connectionToken && (
        <div className="mb-5 rounded-lg border border-amber-300 bg-amber-50 p-4 dark:border-amber-700 dark:bg-amber-900/20">
          <p className="text-sm font-semibold text-amber-900 dark:text-amber-200">Registration token は今だけ表示されます</p>
          <code className="mt-2 block break-all font-mono text-xs text-amber-900 dark:text-amber-200">{connectionToken}</code>
          <button type="button" className="mt-2 text-xs text-amber-900 underline dark:text-amber-200" onClick={() => setConnectionToken("")}>
            閉じる
          </button>
        </div>
      )}

      <SettingsSubsection title="稼働状況" description="Manager の制御チャネル、Runner、ログをライブで確認します">
        <div className="grid gap-3 sm:grid-cols-3">
          {[
            ["Manager online", `${runtimeStatuses.filter((item) => item.online).length}/${runtimeStatuses.length}`],
            ["Running runners", runtimeStatuses.reduce((sum, item) => sum + (item.status?.running_runners || 0), 0)],
            ["Used runners", runtimeStatuses.reduce((sum, item) => sum + (item.status?.used_runners || 0), 0)],
          ].map(([label, value]) => (
            <div key={label} className="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-gray-900">
              <p className="text-xs text-gray-500 dark:text-gray-400">{label}</p>
              <p className="mt-1 text-2xl font-semibold tabular-nums text-gray-950 dark:text-white">{value}</p>
            </div>
          ))}
        </div>
      </SettingsSubsection>

      <SessionPoolsSetup model={model} />

      <SessionPoolManagers model={model} />

      <SessionLogicalPools model={model} />
    </>
  );
}
