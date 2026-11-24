import type { FormEvent } from "react";
import type {
  ClusterSessionManager,
  LogicalSessionPool,
  SessionPoolBinding,
  SessionPoolSupplier,
} from "@/types/session_pool";
import {
  errorMessage,
  type PoolClient,
  type PoolFormState,
  type PoolResources,
  type SetError,
} from "./sessionPoolsAdminState";

type Reload = PoolResources["reload"];

export function createManagerAndPoolActions(
  client: PoolClient,
  forms: PoolFormState,
  reload: Reload,
  setError: SetError,
) {
  const addManager = async (event: FormEvent) => {
    event.preventDefault();
    try {
      const result = await client.createClusterSessionManager(
        forms.managerName,
      );
      forms.setConnectionToken(result.registration_token);
      forms.setManagerName("");
      forms.setManagerID(result.manager.id);
      await reload();
    } catch (reason) {
      setError(errorMessage(reason, "Manager登録に失敗しました"));
    }
  };
  const addLogicalPool = async (event: FormEvent) => {
    event.preventDefault();
    try {
      const pool = await client.createSessionPool({ name: forms.poolName });
      forms.setPoolName("");
      forms.setSupplierPool(pool.name);
      forms.setBindingPool(pool.name);
      await reload();
    } catch (reason) {
      setError(errorMessage(reason, "Logical Pool作成に失敗しました"));
    }
  };
  return { addManager, addLogicalPool };
}

export function createSupplierAndBindingActions(
  client: PoolClient,
  forms: PoolFormState,
  reload: Reload,
  setError: SetError,
) {
  const addSupplier = async (event: FormEvent) => {
    event.preventDefault();
    try {
      await client.createSessionPoolSupplier(forms.managerID, {
        pool: forms.supplierPool,
        min_idle: forms.minIdle,
        max_runners: forms.maxRunners,
      });
      await reload();
    } catch (reason) {
      setError(errorMessage(reason, "Pool Supplier追加に失敗しました"));
    }
  };
  const addBinding = async (event: FormEvent) => {
    event.preventDefault();
    try {
      await client.createSessionPoolBinding(
        forms.bindingPool,
        forms.subjectType,
        forms.subjectID,
        forms.bindingRoles,
        forms.bindingPriority,
        forms.maxConcurrent,
      );
      forms.setSubjectID("");
      await reload();
    } catch (reason) {
      setError(errorMessage(reason, "Pool Binding作成に失敗しました"));
    }
  };
  return { addSupplier, addBinding };
}

export function createManagerAndPoolRemovalActions(
  client: PoolClient,
  reload: Reload,
  setError: SetError,
) {
  const removeManager = async (manager: ClusterSessionManager) => {
    if (
      !window.confirm(
        `Session Manager「${manager.name}」を削除しますか？関連するSupplierと待機Runnerも削除されます。`,
      )
    )
      return;
    try {
      await client.deleteClusterSessionManager(manager.id);
      await reload();
    } catch (reason) {
      setError(errorMessage(reason, "Session Manager削除に失敗しました"));
    }
  };
  const removePool = async (pool: LogicalSessionPool) => {
    if (
      !window.confirm(
        `Logical Pool「${pool.name}」を削除しますか？Supplier、Bindingなどの関連リソースも削除されます。`,
      )
    )
      return;
    try {
      await client.deleteSessionPool(pool.name);
      await reload();
    } catch (reason) {
      setError(errorMessage(reason, "Logical Pool削除に失敗しました"));
    }
  };
  return { removeManager, removePool };
}

export function createSupplierAndBindingRemovalActions(
  client: PoolClient,
  managers: ClusterSessionManager[],
  reload: Reload,
  setError: SetError,
) {
  const removeSupplier = async (supplier: SessionPoolSupplier) => {
    const manager = managers.find((item) => item.id === supplier.manager_id);
    if (
      !window.confirm(
        `${manager?.name || supplier.manager_id} の「${supplier.pool}」Supplierを削除しますか？待機Runnerも削除されます。`,
      )
    )
      return;
    try {
      await client.deleteSessionPoolSupplier(
        supplier.manager_id,
        supplier.pool,
      );
      await reload();
    } catch (reason) {
      setError(errorMessage(reason, "Pool Supplier削除に失敗しました"));
    }
  };
  const removeBinding = async (pool: string, bindingID: string) => {
    try {
      await client.deleteSessionPoolBinding(pool, bindingID);
      await reload();
    } catch (reason) {
      setError(errorMessage(reason, "Pool Binding削除に失敗しました"));
    }
  };
  return { removeSupplier, removeBinding };
}

async function runPatch(
  operation: () => Promise<unknown>,
  reload: Reload,
  setError: SetError,
  fallback: string,
) {
  try {
    await operation();
    await reload();
  } catch (reason) {
    setError(errorMessage(reason, fallback));
    throw reason;
  }
}

export function createPatchActions(
  client: PoolClient,
  reload: Reload,
  setError: SetError,
) {
  const patchPool = (
    pool: LogicalSessionPool,
    patch: { enabled?: boolean; labels?: Record<string, string> },
  ) =>
    runPatch(
      () => client.patchManagedSessionPool(pool.name, patch),
      reload,
      setError,
      "Logical Pool更新に失敗しました",
    );
  const togglePool = (pool: LogicalSessionPool) =>
    patchPool(pool, { enabled: !pool.enabled }).catch(() => undefined);
  const patchSupplier = (
    supplier: SessionPoolSupplier,
    patch: {
      enabled?: boolean;
      draining?: boolean;
      min_idle?: number;
      max_runners?: number;
    },
  ) =>
    runPatch(
      () =>
        client.patchManagedSessionPoolSupplier(
          supplier.pool,
          supplier.manager_id,
          patch,
        ),
      reload,
      setError,
      "Pool Supplier更新に失敗しました",
    );
  const patchBinding = (
    binding: SessionPoolBinding,
    patch: {
      roles?: Array<"use" | "manage">;
      enabled?: boolean;
      priority?: number;
      max_concurrent?: number;
    },
  ) =>
    runPatch(
      () =>
        client.patchManagedSessionPoolBinding(binding.pool, binding.id, patch),
      reload,
      setError,
      "Pool Binding更新に失敗しました",
    );
  return { togglePool, patchPool, patchSupplier, patchBinding };
}
