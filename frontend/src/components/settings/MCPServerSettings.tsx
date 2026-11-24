"use client";

import type { APIMCPServerConfig } from "@/types/settings";
import { MCPServerList } from "./MCPServerList";
import { MCPServerModal } from "./MCPServerModal";
import { useMCPServerSettings } from "./useMCPServerSettings";

interface MCPServerSettingsProps {
  servers: Record<string, APIMCPServerConfig> | undefined;
  onChange: (servers: Record<string, APIMCPServerConfig>) => void;
}

export function MCPServerSettings({ servers, onChange }: MCPServerSettingsProps) {
  const controller = useMCPServerSettings(servers, onChange);
  return (
    <div className="space-y-4">
      <MCPServerList entries={Object.entries(servers || {})} onEdit={controller.handleEdit} onDelete={controller.handleDelete} />
      <button
        type="button"
        onClick={controller.handleAdd}
        className="w-full py-2 px-4 border-2 border-dashed border-gray-300 dark:border-gray-600 rounded-lg text-gray-600 dark:text-gray-400 hover:border-blue-500 hover:text-blue-500 dark:hover:border-blue-400 dark:hover:text-blue-400 transition-colors"
      >
        + Add MCP Server
      </button>
      {controller.editingServer && (
        <MCPServerModal
          server={controller.editingServer}
          existingNames={Object.keys(servers || {})}
          onSave={controller.handleSave}
          onClose={controller.handleClose}
        />
      )}
    </div>
  );
}
