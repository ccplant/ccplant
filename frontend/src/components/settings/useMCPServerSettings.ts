import { useState } from "react";
import type { APIMCPServerConfig } from "@/types/settings";

export interface EditingServer {
  name: string;
  config: APIMCPServerConfig;
  isNew: boolean;
}

const defaultConfig = (): APIMCPServerConfig => ({ type: "stdio", command: "", args: [], env: {} });

export function useMCPServerSettings(servers: Record<string, APIMCPServerConfig> | undefined, onChange: (servers: Record<string, APIMCPServerConfig>) => void) {
  const [editingServer, setEditingServer] = useState<EditingServer | null>(null);
  const handleAdd = () => setEditingServer({ name: "", config: defaultConfig(), isNew: true });
  const handleEdit = (name: string) => {
    const config = servers?.[name];
    if (config) setEditingServer({ name, config: { ...config }, isNew: false });
  };
  const handleDelete = (name: string) => {
    if (!confirm(`Are you sure you want to delete "${name}"?`)) return;
    const updated = { ...servers };
    delete updated[name];
    onChange(updated);
  };
  const handleSave = (name: string, config: APIMCPServerConfig) => {
    const updated = { ...servers };
    if (editingServer && !editingServer.isNew && editingServer.name !== name) delete updated[editingServer.name];
    updated[name] = config;
    onChange(updated);
    setEditingServer(null);
  };
  return { editingServer, handleAdd, handleEdit, handleDelete, handleSave, handleClose: () => setEditingServer(null) };
}
