import { SettingsSubsection } from "@/components/settings";
import type { SessionPoolsAdminModel } from "./useSessionPoolsAdmin";

const input = "w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-transparent focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800 dark:text-white";
const card = "space-y-3 rounded-lg border border-gray-200 p-4 dark:border-gray-700";
const button = "w-fit rounded-md bg-blue-600 px-3 py-1.5 text-sm font-semibold text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50";
const number = "flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md bg-blue-50 text-xs font-bold tabular-nums text-blue-600 dark:bg-blue-900/40 dark:text-blue-300";

function FormTitle({ step, children }: { step: number; children: string }) {
  return (
    <div className="flex items-center gap-2">
      <span className={number}>{step}</span>
      <h4 className="text-sm font-semibold text-gray-900 dark:text-white">{children}</h4>
    </div>
  );
}

function ManagerForm({ model }: { model: SessionPoolsAdminModel }) {
  return (
    <form onSubmit={model.addManager} className={card}>
      <FormTitle step={1}>Session Manager</FormTitle>
      <p className="text-xs text-gray-500 dark:text-gray-400">Pool を供給する実行環境を登録します。</p>
      <input required className={input} value={model.managerName} onChange={(event) => model.setManagerName(event.target.value)} placeholder="Tokyo Kubernetes" />
      <button className={button}>Manager を登録</button>
    </form>
  );
}

function LogicalPoolForm({ model }: { model: SessionPoolsAdminModel }) {
  return (
    <form onSubmit={model.addLogicalPool} className={card}>
      <FormTitle step={2}>Logical Pool</FormTitle>
      <p className="text-xs text-gray-500 dark:text-gray-400">認可やセッション要求が参照するクラスタ共通の Pool です。</p>
      <input required className={input} value={model.poolName} onChange={(event) => model.setPoolName(event.target.value)} placeholder="linux-standard" />
      <button className={button}>Logical Pool を作成</button>
    </form>
  );
}

function SupplierForm({ model }: { model: SessionPoolsAdminModel }) {
  return (
    <form onSubmit={model.addSupplier} className={card}>
      <FormTitle step={3}>Pool Supplier</FormTitle>
      <p className="text-xs text-gray-500 dark:text-gray-400">Manager が供給する Logical Pool とキャパシティを設定します。</p>
      <select required className={input} value={model.managerID} onChange={(event) => model.setManagerID(event.target.value)}>
        <option value="" disabled>
          Manager を選択
        </option>
        {model.managers.map((manager) => (
          <option key={manager.id} value={manager.id}>
            {manager.name}
          </option>
        ))}
      </select>
      <select required className={input} value={model.supplierPool} onChange={(event) => model.setSupplierPool(event.target.value)}>
        <option value="" disabled>
          Logical Pool を選択
        </option>
        {model.pools.map((pool) => (
          <option key={pool.name} value={pool.name}>
            {pool.name}
          </option>
        ))}
      </select>
      <div className="grid grid-cols-2 gap-3">
        <label className="text-xs text-gray-500 dark:text-gray-400">
          Min idle
          <input required min={0} type="number" className={input} value={model.minIdle} onChange={(event) => model.setMinIdle(Number(event.target.value))} />
        </label>
        <label className="text-xs text-gray-500 dark:text-gray-400">
          Max runners
          <input required min={0} type="number" className={input} value={model.maxRunners} onChange={(event) => model.setMaxRunners(Number(event.target.value))} />
        </label>
      </div>
      <button disabled={!model.managerID || !model.supplierPool} className={button}>
        Supplier を追加
      </button>
    </form>
  );
}

function updateSubjectType(model: SessionPoolsAdminModel, value: string) {
  const nextType = value as "user" | "team" | "all";
  model.setSubjectType(nextType);
  if (nextType === "all") {
    model.setSubjectID("");
    model.setBindingRoles(["use"]);
  }
}

function toggleRole(model: SessionPoolsAdminModel, role: "use" | "manage", checked: boolean) {
  model.setBindingRoles((current) => (checked ? [...current, role] : current.filter((item) => item !== role)));
}

function BindingForm({ model }: { model: SessionPoolsAdminModel }) {
  return (
    <form onSubmit={model.addBinding} className={card}>
      <FormTitle step={4}>Pool Binding</FormTitle>
      <p className="text-xs text-gray-500 dark:text-gray-400">Logical Pool を利用できる user、team、またはクラスタ内の全員を指定します。</p>
      <select required className={input} value={model.bindingPool} onChange={(event) => model.setBindingPool(event.target.value)}>
        <option value="" disabled>
          Logical Pool を選択
        </option>
        {model.pools.map((pool) => (
          <option key={pool.name} value={pool.name}>
            {pool.name}
          </option>
        ))}
      </select>
      <select className={input} value={model.subjectType} onChange={(event) => updateSubjectType(model, event.target.value)}>
        <option value="team">Team</option>
        <option value="user">User</option>
        <option value="all">All users and teams</option>
      </select>
      <fieldset className="flex gap-4 rounded-lg border border-gray-300 px-3 py-2 dark:border-gray-700">
        <legend className="px-1 text-xs text-gray-500 dark:text-gray-400">Roles</legend>
        {(["use", "manage"] as const).map((role) => (
          <label key={role} className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input type="checkbox" checked={model.bindingRoles.includes(role)} disabled={role === "manage" && model.subjectType === "all"} onChange={(event) => toggleRole(model, role, event.target.checked)} />
            {role === "use" ? "Use" : "Manage"}
          </label>
        ))}
      </fieldset>
      <input required={model.subjectType !== "all"} disabled={model.subjectType === "all"} className={input} value={model.subjectID} onChange={(event) => model.setSubjectID(event.target.value)} placeholder={model.subjectType === "all" ? "Subject ID は不要です" : model.subjectType === "team" ? "org/team" : "user-id"} />
      <div className="grid grid-cols-2 gap-3">
        <label className="text-xs text-gray-500 dark:text-gray-400">
          Priority
          <input type="number" className={input} value={model.bindingPriority} onChange={(event) => model.setBindingPriority(Number(event.target.value))} />
        </label>
        <label className="text-xs text-gray-500 dark:text-gray-400">
          Max concurrent
          <input min={0} type="number" className={input} value={model.maxConcurrent} onChange={(event) => model.setMaxConcurrent(Number(event.target.value))} />
        </label>
      </div>
      <button disabled={!model.bindingPool || model.bindingRoles.length === 0} className={button}>
        Binding を追加
      </button>
    </form>
  );
}

export function SessionPoolsSetup({ model }: { model: SessionPoolsAdminModel }) {
  return (
    <SettingsSubsection title="セットアップ" description="Manager から Binding まで、上から順に登録します">
      <div className="grid gap-4 md:grid-cols-2">
        <ManagerForm model={model} />
        <LogicalPoolForm model={model} />
        <SupplierForm model={model} />
        <BindingForm model={model} />
      </div>
    </SettingsSubsection>
  );
}
