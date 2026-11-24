import { type ReactNode, useEffect, useRef, useState } from "react";
import { ItemList, ItemListEmpty, SettingsSubsection, StatusBadge } from "@/components/settings";
import { BindingSettingsEditor, PoolSettingsEditor, SupplierSettingsEditor } from "@/components/settings/PoolResourceEditors";
import type { LogicalSessionPool, SessionPoolBinding, SessionPoolSupplier } from "@/types/session_pool";
import type { SessionPoolsAdminModel } from "./useSessionPoolsAdmin";

const menuItemClass = "block w-full px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-gray-800";

function RowActionsMenu({ label, children }: { label: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => {
      if (!containerRef.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);
  return (
    <div ref={containerRef} className="relative inline-flex">
      <button type="button" aria-label={label} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((current) => !current)} className="rounded-md p-1.5 text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:hover:bg-gray-800 dark:hover:text-white">
        •••
      </button>
      {open && (
        <div role="menu" className="absolute right-0 top-full z-20 mt-1 w-32 rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
          {children}
        </div>
      )}
    </div>
  );
}

function PoolHeader({ pool, model }: { pool: LogicalSessionPool; model: SessionPoolsAdminModel }) {
  return (
    <div className="flex flex-col gap-4 border-b border-gray-100 pb-4 dark:border-gray-800 sm:flex-row sm:items-start sm:justify-between">
      <div className="min-w-0 space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="text-lg font-semibold text-gray-950 dark:text-white">{pool.name}</h3>
          <StatusBadge tone={pool.enabled ? "green" : "amber"}>{pool.enabled ? "Enabled" : "Disabled"}</StatusBadge>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {Object.entries(pool.labels ?? {}).map(([key, value]) => (
            <code key={key} className="rounded-md bg-gray-100 px-2 py-1 text-[11px] text-gray-700 dark:bg-gray-800 dark:text-gray-300">
              {key}={value}
            </code>
          ))}
          {Object.keys(pool.labels ?? {}).length === 0 && <span className="text-xs text-gray-400">Labelsなし</span>}
        </div>
      </div>
      <div className="flex shrink-0 flex-wrap items-center gap-3">
        <PoolSettingsEditor pool={pool} onSave={(patch) => model.patchPool(pool, patch)} />
        <button type="button" onClick={() => void model.togglePool(pool)} className="text-xs text-blue-700 dark:text-blue-300">
          {pool.enabled ? "停止" : "有効化"}
        </button>
        <button type="button" onClick={() => void model.removePool(pool)} className="text-xs text-red-600 dark:text-red-400">
          削除
        </button>
      </div>
    </div>
  );
}

function SupplierRow({ supplier, model }: { supplier: SessionPoolSupplier; model: SessionPoolsAdminModel }) {
  const name = model.managers.find((manager) => manager.id === supplier.manager_id)?.name || supplier.manager_id;
  const active = supplier.enabled && !supplier.draining;
  return (
    <tr>
      <td className="max-w-48 px-3 py-3 font-medium text-gray-900 dark:text-white">
        <span className="break-words">{name}</span>
      </td>
      <td className="px-3 py-3">
        <StatusBadge tone={active ? "green" : "amber"}>{active ? "有効" : "無効"}</StatusBadge>
      </td>
      <td className="whitespace-nowrap px-3 py-3 text-xs text-gray-500">
        {supplier.min_idle ?? 0} min / {supplier.max_runners || "∞"} max
      </td>
      <td className="px-2 py-2 text-right">
        <RowActionsMenu label={`${supplier.pool}のSupplier操作`}>
          <SupplierSettingsEditor supplier={supplier} onSave={(patch) => model.patchSupplier(supplier, patch)} triggerClassName={menuItemClass} />
          <button type="button" className={menuItemClass} onClick={() => void model.patchSupplier(supplier, { enabled: !active, draining: false }).catch(() => undefined)}>
            {active ? "無効化" : "有効化"}
          </button>
          <button type="button" className={`${menuItemClass} text-red-600 dark:text-red-400`} onClick={() => void model.removeSupplier(supplier)}>
            削除
          </button>
        </RowActionsMenu>
      </td>
    </tr>
  );
}

function SuppliersTable({ suppliers, model }: { suppliers: SessionPoolSupplier[]; model: SessionPoolsAdminModel }) {
  return (
    <div>
      <div className="mb-3 flex items-center justify-between">
        <h4 className="text-sm font-semibold text-gray-800 dark:text-gray-200">Suppliers</h4>
        <span className="text-xs text-gray-400">{suppliers.length}件</span>
      </div>
      {suppliers.length === 0 && <EmptyResource />}
      {suppliers.length > 0 && (
        <div className="overflow-visible rounded-xl border border-gray-200 dark:border-gray-700">
          <table className="w-full table-fixed text-left text-sm">
            <thead className="bg-gray-50 text-[11px] uppercase tracking-wide text-gray-500 dark:bg-gray-950/60">
              <tr>
                <th className="px-3 py-2 font-medium">Manager</th>
                <th className="w-20 px-3 py-2 font-medium">状態</th>
                <th className="w-28 px-3 py-2 font-medium">Capacity</th>
                <th className="w-10 px-2 py-2">
                  <span className="sr-only">操作</span>
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200 dark:divide-gray-800">
              {suppliers.map((supplier) => (
                <SupplierRow key={supplier.manager_id} supplier={supplier} model={model} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function BindingRow({ binding, model }: { binding: SessionPoolBinding; model: SessionPoolsAdminModel }) {
  return (
    <tr>
      <td className="max-w-48 px-3 py-3 font-medium text-gray-900 dark:text-white">
        <span className="break-words">
          {binding.subject_type}: {binding.subject_id || "everyone"}
        </span>
      </td>
      <td className="px-3 py-3 text-xs text-gray-600 dark:text-gray-300">{binding.roles?.join(", ") || binding.role}</td>
      <td className="whitespace-nowrap px-3 py-3 text-xs text-gray-500">{binding.max_concurrent ? `${binding.max_concurrent} max` : "無制限"}</td>
      <td className="px-2 py-2 text-right">
        <RowActionsMenu label={`${binding.subject_id || "everyone"}のBinding操作`}>
          <BindingSettingsEditor binding={binding} onSave={(patch) => model.patchBinding(binding, patch)} triggerClassName={menuItemClass} />
          <button type="button" className={`${menuItemClass} text-red-600 dark:text-red-400`} onClick={() => void model.removeBinding(binding.pool, binding.id)}>
            削除
          </button>
        </RowActionsMenu>
      </td>
    </tr>
  );
}

function BindingsTable({ bindings, model }: { bindings: SessionPoolBinding[]; model: SessionPoolsAdminModel }) {
  return (
    <div>
      <div className="mb-3 flex items-center justify-between">
        <h4 className="text-sm font-semibold text-gray-800 dark:text-gray-200">Bindings</h4>
        <span className="text-xs text-gray-400">{bindings.length}件</span>
      </div>
      {bindings.length === 0 && <EmptyResource />}
      {bindings.length > 0 && (
        <div className="overflow-visible rounded-xl border border-gray-200 dark:border-gray-700">
          <table className="w-full table-fixed text-left text-sm">
            <thead className="bg-gray-50 text-[11px] uppercase tracking-wide text-gray-500 dark:bg-gray-950/60">
              <tr>
                <th className="px-3 py-2 font-medium">Subject</th>
                <th className="w-24 px-3 py-2 font-medium">Roles</th>
                <th className="w-20 px-3 py-2 font-medium">Limit</th>
                <th className="w-10 px-2 py-2">
                  <span className="sr-only">操作</span>
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200 dark:divide-gray-800">
              {bindings.map((binding) => (
                <BindingRow key={binding.id} binding={binding} model={model} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function EmptyResource() {
  return <div className="rounded-xl border border-dashed border-gray-200 px-4 py-6 text-center text-sm text-gray-400 dark:border-gray-700">未設定</div>;
}

function PoolCard({ pool, model }: { pool: LogicalSessionPool; model: SessionPoolsAdminModel }) {
  const suppliers = model.suppliers.filter((supplier) => supplier.pool === pool.name);
  const bindings = model.bindings[pool.name] || [];
  return (
    <section className="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-gray-800 dark:bg-gray-900">
      <PoolHeader pool={pool} model={model} />
      <div className="mt-5 space-y-5">
        <SuppliersTable suppliers={suppliers} model={model} />
        <BindingsTable bindings={bindings} model={model} />
      </div>
    </section>
  );
}

export function SessionLogicalPools({ model }: { model: SessionPoolsAdminModel }) {
  return (
    <SettingsSubsection title="Logical Pools" description="Pool ごとの供給元と利用権限">
      {model.pools.length === 0 && (
        <ItemList>
          <ItemListEmpty>Logical Pool はまだありません</ItemListEmpty>
        </ItemList>
      )}
      {model.pools.length > 0 && (
        <div className="space-y-4">
          {model.pools.map((pool) => (
            <PoolCard key={pool.name} pool={pool} model={model} />
          ))}
        </div>
      )}
    </SettingsSubsection>
  );
}
