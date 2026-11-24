import { useMemo, useState } from "react";
import { createCurrentDeploymentAgentAPIProxyClient } from "@/lib/agentapi-proxy-client";
import {
  createManagerAndPoolActions,
  createManagerAndPoolRemovalActions,
  createPatchActions,
  createSupplierAndBindingActions,
  createSupplierAndBindingRemovalActions,
} from "./sessionPoolsAdminActions";
import {
  usePoolFormState,
  usePoolLogs,
  usePoolResources,
} from "./sessionPoolsAdminState";

export function useSessionPoolsAdmin() {
  const client = useMemo(
    () => createCurrentDeploymentAgentAPIProxyClient(),
    [],
  );
  const [error, setError] = useState("");
  const forms = usePoolFormState();
  const resources = usePoolResources(client, forms, setError);
  const logs = usePoolLogs(client, setError);
  const createPrimary = createManagerAndPoolActions(
    client,
    forms,
    resources.reload,
    setError,
  );
  const createRelated = createSupplierAndBindingActions(
    client,
    forms,
    resources.reload,
    setError,
  );
  const removePrimary = createManagerAndPoolRemovalActions(
    client,
    resources.reload,
    setError,
  );
  const removeRelated = createSupplierAndBindingRemovalActions(
    client,
    resources.managers,
    resources.reload,
    setError,
  );
  const patches = createPatchActions(client, resources.reload, setError);
  return {
    ...resources,
    ...forms,
    ...logs,
    ...createPrimary,
    ...createRelated,
    ...removePrimary,
    ...removeRelated,
    ...patches,
    error,
  };
}

export type SessionPoolsAdminModel = ReturnType<typeof useSessionPoolsAdmin>;
