use crate::binary::{run_native_json, run_native_plain};
use crate::types::{CommandResult, DoctorResult, InstallRequest, NativeSession, NativeStatus};
use serde::de::DeserializeOwned;
use tauri::AppHandle;
use tauri_plugin_shell::ShellExt;

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
pub async fn native_status(app: AppHandle) -> Result<NativeStatus, String> {
    let stdout = run_native_json(&app, "status").await?;
    parse_json::<NativeStatus>(&stdout, "status")
}

/// `agentapi-proxy native sessions --json`.
#[tauri::command]
pub async fn native_sessions(app: AppHandle) -> Result<Vec<NativeSession>, String> {
    let stdout = run_native_json(&app, "sessions").await?;
    // `native sessions` is an alias of `session-list`; both emit a JSON array.
    parse_json::<Vec<NativeSession>>(&stdout, "sessions")
}

/// `agentapi-proxy native doctor --json`.
///
#[tauri::command]
pub async fn native_doctor(app: AppHandle) -> Result<DoctorResult, String> {
    let stdout = run_native_json(&app, "doctor").await?;
    parse_json::<DoctorResult>(&stdout, "doctor")
}

/// `agentapi-proxy native restart`.
#[tauri::command]
pub async fn native_restart(app: AppHandle) -> Result<CommandResult, String> {
    let (ok, message) = run_native_plain(&app, "restart").await;
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

/// Register and install the per-user LaunchAgent using the bundled CLI.
#[tauri::command]
pub async fn native_install(
    app: AppHandle,
    request: InstallRequest,
) -> Result<CommandResult, String> {
    let upstream = request.upstream.trim();
    let public_url = request.public_url.trim();
    let name = request.name.trim();
    if !is_http_url(upstream) || !is_http_url(public_url) {
        return Err("upstream and public URL must start with http:// or https://".to_string());
    }
    if name.is_empty() || request.api_key.trim().is_empty() {
        return Err("name and API key are required".to_string());
    }

    let mut args = vec![
        "native",
        "install",
        "--upstream",
        upstream,
        "--public-url",
        public_url,
        "--name",
        name,
        "--api-key-env",
        "AGENTAPI_NATIVE_INSTALL_KEY",
    ];
    if request.filesystem_sandbox {
        args.push("--filesystem-sandbox");
    }
    let output = app
        .shell()
        .sidecar("agentapi-proxy")
        .map_err(|e| format!("bundled agentapi-proxy is unavailable: {e}"))?
        .env("AGENTAPI_NATIVE_INSTALL_KEY", request.api_key)
        .args(args)
        .output()
        .await
        .map_err(|e| format!("failed to run native install: {e}"))?;
    let message = String::from_utf8_lossy(if output.stderr.is_empty() {
        &output.stdout
    } else {
        &output.stderr
    })
    .trim()
    .to_string();
    Ok(CommandResult {
        ok: output.status.success(),
        message,
    })
}

fn is_http_url(value: &str) -> bool {
    value.starts_with("https://") || value.starts_with("http://")
}

#[cfg(test)]
mod tests {
    use super::{is_http_url, parse_json};
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

    #[test]
    fn accepts_only_http_install_urls() {
        assert!(is_http_url("https://parent.example"));
        assert!(is_http_url("http://10.0.0.10:8080"));
        assert!(!is_http_url("file:///tmp/socket"));
    }
}
