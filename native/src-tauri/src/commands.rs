use crate::binary::{run_native_json, run_native_plain};
use crate::types::{CommandResult, DoctorResult, NativeSession, NativeStatus};
use serde::de::DeserializeOwned;

/// Parse the stdout of a `native <sub> --json` command into a typed value.
/// Tolerates leading/trailing whitespace and empty output (treated as an
/// explicit error so the UI can surface it).
fn parse_json<T: DeserializeOwned>(stdout: &[u8], sub: &str) -> Result<T, String> {
    let trimmed = stdout
        .iter()
        .skip_while(|b| b.is_ascii_whitespace())
        .copied()
        .collect::<Vec<_>>();
    if trimmed.is_empty() {
        return Err(format!("`native {sub} --json` produced no output"));
    }
    serde_json::from_slice::<T>(&trimmed)
        .map_err(|e| format!("failed to parse `native {sub} --json` output: {e}"))
}

/// `agentapi-proxy native status --json`.
#[tauri::command]
pub fn native_status() -> Result<NativeStatus, String> {
    let stdout = run_native_json("status")?;
    parse_json::<NativeStatus>(&stdout, "status")
}

/// `agentapi-proxy native sessions --json`.
#[tauri::command]
pub fn native_sessions() -> Result<Vec<NativeSession>, String> {
    let stdout = run_native_json("sessions")?;
    // `native sessions` is an alias of `session-list`; both emit a JSON array.
    parse_json::<Vec<NativeSession>>(&stdout, "sessions")
}

/// `agentapi-proxy native doctor --json`.
///
#[tauri::command]
pub fn native_doctor() -> Result<DoctorResult, String> {
    let stdout = run_native_json("doctor")?;
    parse_json::<DoctorResult>(&stdout, "doctor")
}

/// `agentapi-proxy native restart`.
#[tauri::command]
pub fn native_restart() -> Result<CommandResult, String> {
    let (ok, message) = run_native_plain("restart");
    Ok(CommandResult {
        ok,
        message: if message.is_empty() {
            if ok {
                "Daemon restarted.".to_string()
            } else {
                "Restart failed.".to_string()
            }
        } else {
            message
        },
    })
}

#[cfg(test)]
mod tests {
    use super::parse_json;
    use crate::types::{DoctorResult, NativeStatus};

    #[test]
    fn parses_status_contract() {
        let json = br#"{
          "service":"running","manager_id":"manager-1","upstream":"https://parent.example",
          "public_url":"https://mac.example","labels":{"os":"darwin"},"version":"dev",
          "filesystem_sandbox":true,"active_sessions":2,"health":"ok","state":"/tmp/state"
        }"#;
        let status = parse_json::<NativeStatus>(json, "status").expect("status JSON");
        assert_eq!(status.manager_id, "manager-1");
        assert_eq!(status.active_sessions, 2);
    }

    #[test]
    fn parses_doctor_checks() {
        let json = br#"{"ok":false,"checks":[{"id":"local_health","status":"error","message":"unreachable"}]}"#;
        let doctor = parse_json::<DoctorResult>(json, "doctor").expect("doctor JSON");
        assert!(!doctor.ok);
        assert_eq!(doctor.checks[0].id, "local_health");
    }

    #[test]
    fn rejects_empty_json() {
        assert!(parse_json::<NativeStatus>(b"  \n", "status").is_err());
    }
}
