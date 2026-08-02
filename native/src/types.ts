// Shared types that mirror the Rust structs in src-tauri/src/types.rs.
// Keep these in sync with the serde JSON produced by the Tauri commands.

export interface NativeStatus {
  service: string;
  manager_id: string;
  upstream: string;
  public_url: string;
  labels: Record<string, string>;
  version: string;
  filesystem_sandbox: boolean;
  active_sessions: number;
  health: string;
  state: string;
}

export interface NativeSession {
  id: string;
  pid: number;
  started_at: string;
  status: string;
  log_path: string;
}

export interface DoctorResult {
  ok: boolean;
  checks: DoctorCheck[];
}

export interface DoctorCheck {
  id: string;
  status: "ok" | "error";
  message: string;
}

export interface RestartResult {
  ok: boolean;
  message: string;
}

export interface InstallRequest {
  upstream: string;
  publicUrl: string;
  name: string;
  apiKey: string;
  filesystemSandbox: boolean;
  setupNodejs: boolean;
  nodejsVersion: string;
}

export interface ResetRequest {
  force: boolean;
}

export type DashboardData = {
  status: NativeStatus | null;
  sessions: NativeSession[];
  doctor: DoctorResult | null;
  warnings?: string[];
};
