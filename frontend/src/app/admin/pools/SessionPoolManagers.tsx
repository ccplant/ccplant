import { Terminal } from "lucide-react";
import { ItemList, ItemListEmpty, ItemListRow, RowAction, SettingsSubsection, StatusBadge } from "@/components/settings";
import type { SessionPoolLogs, SessionPoolManagerStatus } from "@/types/session_pool";
import type { SessionPoolsAdminModel } from "./useSessionPoolsAdmin";

function LogPanel({ target, result, onClose }: { target: string; result: SessionPoolLogs | null; onClose: () => void }) {
  return (
    <div className="mt-3 rounded-lg border border-gray-700 bg-gray-950 p-3 text-gray-100">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate font-mono text-xs">{target}</p>
          {result?.source && <p className="truncate font-mono text-[10px] text-gray-500">{result.source}</p>}
        </div>
        <button type="button" onClick={onClose} className="text-xs text-gray-400 hover:text-white">
          閉じる
        </button>
      </div>
      <pre className="max-h-80 overflow-auto whitespace-pre-wrap rounded bg-black p-2 font-mono text-[11px] leading-4 text-gray-300">{result ? (result.lines.length ? result.lines.join("\n") : "ログはありません") : "ログを読み込み中..."}</pre>
    </div>
  );
}

function ManagerBadges({ runtime }: { runtime?: SessionPoolManagerStatus }) {
  return (
    <>
      <StatusBadge tone={runtime?.online ? "green" : "amber"}>{runtime ? (runtime.online ? "Online" : "Offline") : "未割り当て"}</StatusBadge>
      {runtime?.pools.map((pool) => (
        <StatusBadge key={pool} tone="blue">
          {pool}
        </StatusBadge>
      ))}
      {runtime?.status && (
        <StatusBadge tone="violet">
          {runtime.status.used_runners || 0}/{runtime.status.running_runners || 0} used
        </StatusBadge>
      )}
    </>
  );
}

function RunnerButtons({ runnerIDs, showLogs }: { runnerIDs: string[]; showLogs: SessionPoolsAdminModel["showLogs"] }) {
  if (runnerIDs.length === 0) return null;
  return (
    <div className="mt-3 flex flex-wrap items-center gap-2">
      <span className="text-xs font-semibold text-gray-500 dark:text-gray-400">Runners</span>
      {runnerIDs.map((runnerID) => (
        <button key={runnerID} type="button" onClick={() => void showLogs("runner", runnerID)} className="inline-flex items-center gap-1 rounded-md bg-gray-100 px-2 py-1 font-mono text-xs text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700">
          <Terminal className="h-3 w-3" />
          {runnerID}
        </button>
      ))}
    </div>
  );
}

function ManagerRow({ model, index }: { model: SessionPoolsAdminModel; index: number }) {
  const manager = model.managers[index];
  const runtime = model.runtimeStatuses.find((item) => item.manager.id === manager.id);
  const runnerIDs = runtime?.status?.running_runner_ids || [];
  const closeLogs = () => {
    model.setLogTarget(null);
    model.setLogResult(null);
  };
  return (
    <ItemListRow
      name={manager.name}
      meta={`${manager.id}${runtime?.status?.version ? ` · ${runtime.status.version}` : ""}`}
      badges={<ManagerBadges runtime={runtime} />}
      actions={
        <>
          <RowAction onClick={() => void model.showLogs("manager", manager.id)} disabled={!runtime?.online}>
            ログ
          </RowAction>
          <RowAction tone="danger" onClick={() => void model.removeManager(manager)} title={`${manager.name}を削除`}>
            削除
          </RowAction>
        </>
      }
    >
      {runtime?.error && <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">{runtime.error}</p>}
      <RunnerButtons runnerIDs={runnerIDs} showLogs={model.showLogs} />
      {model.logTarget === `manager:${manager.id}` && <LogPanel target={manager.name} result={model.logResult} onClose={closeLogs} />}
      {runnerIDs.some((runnerID) => model.logTarget === `runner:${runnerID}`) && <LogPanel target={model.logTarget?.slice("runner:".length) || ""} result={model.logResult} onClose={closeLogs} />}
    </ItemListRow>
  );
}

export function SessionPoolManagers({ model }: { model: SessionPoolsAdminModel }) {
  return (
    <SettingsSubsection title="Session Managers">
      <ItemList>
        {model.managers.length === 0 && <ItemListEmpty>Session Manager はまだありません</ItemListEmpty>}
        {model.managers.map((manager, index) => (
          <ManagerRow key={manager.id} model={model} index={index} />
        ))}
      </ItemList>
    </SettingsSubsection>
  );
}
