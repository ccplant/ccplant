import { useCallback, useEffect, useState } from "react";
import type { createCurrentDeploymentAgentAPIProxyClient } from "@/lib/agentapi-proxy-client";
import type {
  ClusterSessionManager,
  LogicalSessionPool,
  SessionPoolBinding,
  SessionPoolLogs,
  SessionPoolManagerStatus,
  SessionPoolSupplier,
} from "@/types/session_pool";

export type PoolClient = ReturnType<
  typeof createCurrentDeploymentAgentAPIProxyClient
>;
export type BindingRole = "use" | "manage";
export type SetError = (message: string) => void;

export function errorMessage(reason: unknown, fallback: string): string {
  return reason instanceof Error ? reason.message : fallback;
}

export function usePoolFormState() {
  const [managerName, setManagerName] = useState("");
  const [managerID, setManagerID] = useState("");
  const [poolName, setPoolName] = useState("");
  const [supplierPool, setSupplierPool] = useState("");
  const [minIdle, setMinIdle] = useState(1);
  const [maxRunners, setMaxRunners] = useState(10);
  const [subjectType, setSubjectType] = useState<"user" | "team" | "all">(
    "team",
  );
  const [subjectID, setSubjectID] = useState("");
  const [bindingRoles, setBindingRoles] = useState<BindingRole[]>(["use"]);
  const [bindingPool, setBindingPool] = useState("");
  const [bindingPriority, setBindingPriority] = useState(0);
  const [maxConcurrent, setMaxConcurrent] = useState(0);
  const [connectionToken, setConnectionToken] = useState("");
  return {
    managerName,
    setManagerName,
    managerID,
    setManagerID,
    poolName,
    setPoolName,
    supplierPool,
    setSupplierPool,
    minIdle,
    setMinIdle,
    maxRunners,
    setMaxRunners,
    subjectType,
    setSubjectType,
    subjectID,
    setSubjectID,
    bindingRoles,
    setBindingRoles,
    bindingPool,
    setBindingPool,
    bindingPriority,
    setBindingPriority,
    maxConcurrent,
    setMaxConcurrent,
    connectionToken,
    setConnectionToken,
  };
}

export type PoolFormState = ReturnType<typeof usePoolFormState>;

interface ResourceSnapshot {
  managers: ClusterSessionManager[];
  pools: LogicalSessionPool[];
  suppliers: SessionPoolSupplier[];
  bindings: Record<string, SessionPoolBinding[]>;
  runtimeStatuses: SessionPoolManagerStatus[];
}

const emptySnapshot: ResourceSnapshot = {
  managers: [],
  pools: [],
  suppliers: [],
  bindings: {},
  runtimeStatuses: [],
};

async function loadResourceSnapshot(
  client: PoolClient,
): Promise<ResourceSnapshot> {
  const [managers, pools, status] = await Promise.all([
    client.listClusterSessionManagers(),
    client.listSessionPools(),
    client
      .getSessionPoolStatus()
      .catch(() => ({ session_pools: [], session_managers: [] })),
  ]);
  const suppliers = (
    await Promise.all(
      managers.map((manager) => client.listSessionPoolSuppliers(manager.id)),
    )
  ).flat();
  const pairs = await Promise.all(
    pools.map(
      async (pool) =>
        [pool.name, await client.listSessionPoolBindings(pool.name)] as const,
    ),
  );
  return {
    managers,
    pools,
    suppliers,
    bindings: Object.fromEntries(pairs),
    runtimeStatuses: status.session_managers,
  };
}

export function usePoolResources(
  client: PoolClient,
  forms: PoolFormState,
  setError: SetError,
) {
  const [snapshot, setSnapshot] = useState(emptySnapshot);
  const [loading, setLoading] = useState(true);
  const { setBindingPool, setManagerID, setSupplierPool } = forms;
  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const next = await loadResourceSnapshot(client);
      setSnapshot(next);
      setManagerID((current) => current || next.managers[0]?.id || "");
      setSupplierPool((current) => current || next.pools[0]?.name || "");
      setBindingPool((current) => current || next.pools[0]?.name || "");
      setError("");
    } catch (reason) {
      setError(errorMessage(reason, "読み込みに失敗しました"));
    } finally {
      setLoading(false);
    }
  }, [client, setBindingPool, setManagerID, setSupplierPool, setError]);
  useEffect(() => void reload(), [reload]);
  return { ...snapshot, loading, reload };
}

export type PoolResources = ReturnType<typeof usePoolResources>;

export function usePoolLogs(client: PoolClient, setError: SetError) {
  const [logTarget, setLogTarget] = useState<string | null>(null);
  const [logResult, setLogResult] = useState<SessionPoolLogs | null>(null);
  const showLogs = async (kind: "manager" | "runner", id: string) => {
    setLogTarget(`${kind}:${id}`);
    setLogResult(null);
    setError("");
    try {
      setLogResult(
        kind === "manager"
          ? await client.getSessionPoolManagerLogs(id)
          : await client.getSessionPoolRunnerLogs(id),
      );
    } catch (reason) {
      setError(errorMessage(reason, "ログの取得に失敗しました"));
    }
  };
  return { logTarget, logResult, setLogTarget, setLogResult, showLogs };
}
