import { useState } from "react";
import type { APIMCPServerConfig } from "@/types/settings";
import type { EditingServer } from "./useMCPServerSettings";

export interface ConfigPair {
  key: string;
  value: string;
}

function initialPairs(values?: Record<string, string>, keys?: string[]): ConfigPair[] {
  if (values && Object.keys(values).length > 0) return Object.entries(values).map(([key, value]) => ({ key, value }));
  return keys?.map((key) => ({ key, value: "" })) || [];
}

function validate(server: EditingServer, existingNames: string[], name: string, config: APIMCPServerConfig): string | null {
  const normalized = name.trim();
  if (!normalized) return "Server name is required";
  if ((server.isNew || server.name !== normalized) && existingNames.includes(normalized)) return "A server with this name already exists";
  if (config.type === "stdio" && !config.command?.trim()) return "Command is required for stdio type";
  if (config.type !== "stdio" && !config.url?.trim()) return "URL is required for http/sse type";
  return null;
}

function pairsToRecord(pairs: ConfigPair[]): Record<string, string> | undefined {
  const entries = pairs.flatMap(({ key, value }) => (key.trim() && value.trim() ? [[key.trim(), value.trim()] as const] : []));
  return entries.length > 0 ? Object.fromEntries(entries) : undefined;
}

function buildConfig(config: APIMCPServerConfig, argsText: string, envPairs: ConfigPair[], headerPairs: ConfigPair[]): APIMCPServerConfig {
  const result: APIMCPServerConfig = { type: config.type };
  if (config.type === "stdio") {
    result.command = config.command?.trim();
    const args = argsText
      .split("\n")
      .map((arg) => arg.trim())
      .filter(Boolean);
    if (args.length > 0) result.args = args;
  } else {
    result.url = config.url?.trim();
    result.headers = pairsToRecord(headerPairs);
  }
  result.env = pairsToRecord(envPairs);
  return result;
}

function updatePair(pairs: ConfigPair[], index: number, field: "key" | "value", value: string): ConfigPair[] {
  return pairs.map((pair, current) => (current === index ? { ...pair, [field]: value } : pair));
}

export function useMCPServerForm(server: EditingServer, existingNames: string[], onSave: (name: string, config: APIMCPServerConfig) => void) {
  const [name, setName] = useState(server.name);
  const [config, setConfig] = useState<APIMCPServerConfig>(server.config);
  const [argsText, setArgsText] = useState(server.config.args?.join("\n") || "");
  const [envPairs, setEnvPairs] = useState(() => initialPairs(server.config.env, server.config.env_keys));
  const [headerPairs, setHeaderPairs] = useState(() => initialPairs(server.config.headers, server.config.header_keys));
  const [error, setError] = useState<string | null>(null);
  const handleTypeChange = (type: APIMCPServerConfig["type"]) => {
    setConfig((previous) => ({
      ...previous,
      type,
      command: type === "stdio" ? previous.command : undefined,
      args: type === "stdio" ? previous.args : undefined,
      url: type !== "stdio" ? previous.url : undefined,
      headers: type !== "stdio" ? previous.headers : undefined,
    }));
    if (type !== "stdio") setArgsText("");
  };
  const handleSave = () => {
    const validationError = validate(server, existingNames, name, config);
    setError(validationError);
    if (!validationError) onSave(name.trim(), buildConfig(config, argsText, envPairs, headerPairs));
  };
  return {
    name,
    setName,
    config,
    setConfig,
    argsText,
    setArgsText,
    envPairs,
    headerPairs,
    error,
    handleTypeChange,
    handleSave,
    handleAddEnv: () => setEnvPairs((pairs) => [...pairs, { key: "", value: "" }]),
    handleRemoveEnv: (index: number) => setEnvPairs((pairs) => pairs.filter((_, current) => current !== index)),
    handleEnvChange: (index: number, field: "key" | "value", value: string) => setEnvPairs((pairs) => updatePair(pairs, index, field, value)),
    handleAddHeader: () => setHeaderPairs((pairs) => [...pairs, { key: "", value: "" }]),
    handleRemoveHeader: (index: number) => setHeaderPairs((pairs) => pairs.filter((_, current) => current !== index)),
    handleHeaderChange: (index: number, field: "key" | "value", value: string) => setHeaderPairs((pairs) => updatePair(pairs, index, field, value)),
  };
}

export type MCPServerFormModel = ReturnType<typeof useMCPServerForm>;
