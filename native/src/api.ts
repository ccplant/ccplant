import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import type {
  DashboardData,
  DoctorResult,
  NativeSession,
  NativeStatus,
  RestartResult,
} from "./types";

// Thin wrapper around `invoke` so the rest of the UI never touches the IPC
// layer directly. All Tauri command names live here.

export async function fetchStatus(): Promise<NativeStatus> {
  return invoke<NativeStatus>("native_status");
}

export async function fetchSessions(): Promise<NativeSession[]> {
  return invoke<NativeSession[]>("native_sessions");
}

export async function fetchDoctor(): Promise<DoctorResult> {
  return invoke<DoctorResult>("native_doctor");
}

export async function restartDaemon(): Promise<RestartResult> {
  return invoke<RestartResult>("native_restart");
}

export async function showDashboard(): Promise<void> {
  await invoke("show_dashboard");
}

/** Load every dashboard panel in parallel. Partial failures are surfaced. */
export async function fetchDashboard(includeDoctor = true): Promise<DashboardData> {
  const [status, sessions, doctor] = await Promise.allSettled([
    fetchStatus(),
    fetchSessions(),
    includeDoctor ? fetchDoctor() : Promise.resolve(null),
  ]);

  const data: DashboardData = {
    status: status.status === "fulfilled" ? status.value : null,
    sessions: sessions.status === "fulfilled" ? sessions.value : [],
    doctor: doctor.status === "fulfilled" ? doctor.value : null,
  };

  // If everything rejected, throw the first error so the caller can render
  // a global error state. Otherwise return partial data plus the aggregated
  // error message via a thrown property for non-fatal warnings.
  const errors: string[] = [];
  for (const [result, name] of [
    [status, "status"],
    [sessions, "sessions"],
    [doctor, "doctor"],
  ] as const) {
    if (result.status === "rejected") {
      errors.push(`${name}: ${String(result.reason)}`);
    }
  }

  if (data.status === null && data.sessions.length === 0 && data.doctor === null) {
    throw new Error(errors.join("; ") || "All panels failed to load");
  }
  data.warnings = errors;
  return data;
}

/** Subscribe to tray-driven refresh events. Returns an unsubscribe function. */
export async function onRefreshRequest(handler: () => void): Promise<UnlistenFn> {
  return listen("dashboard://refresh", () => handler());
}
