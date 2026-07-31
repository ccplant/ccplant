use std::path::PathBuf;
use std::process::Command;

use tauri::AppHandle;
use tauri_plugin_shell::ShellExt;

/// Environment variable that overrides the agentapi-proxy binary location.
pub const BINARY_ENV: &str = "AGENTAPI_PROXY_NATIVE_BINARY";

/// The executable name looked up on `PATH` when the env override is unset.
pub const BINARY_NAME: &str = "agentapi-proxy";

/// The `native` subcommand used by every dashboard query.
const NATIVE_SUBCOMMAND: &str = "native";

/// Resolve the agentapi-proxy binary path.
///
/// Order of precedence:
/// 1. `AGENTAPI_PROXY_NATIVE_BINARY` (absolute path, must exist).
/// 2. The binary managed by `native install` on macOS.
/// 3. `agentapi-proxy` looked up on `PATH`.
///
/// Returns a descriptive error so the UI can render guidance.
pub fn resolve_binary() -> Result<PathBuf, String> {
    if let Ok(raw) = std::env::var(BINARY_ENV) {
        let trimmed = raw.trim();
        if trimmed.is_empty() {
            return Err(format!("{BINARY_ENV} is set but empty"));
        }
        let path = PathBuf::from(trimmed);
        if !path.exists() {
            return Err(format!(
                "{BINARY_ENV} points at {:?} which does not exist",
                path.display()
            ));
        }
        return Ok(path);
    }

    if let Some(home) = std::env::var_os("HOME") {
        let managed = PathBuf::from(home)
            .join("Library")
            .join("Application Support")
            .join("agentapi-native")
            .join("bin")
            .join(BINARY_NAME);
        if is_executable(&managed) {
            return Ok(managed);
        }
    }

    find_on_path(BINARY_NAME).ok_or_else(|| {
        format!("could not find `{BINARY_NAME}` on PATH; set {BINARY_ENV} to the proxy binary")
    })
}

/// Minimal `which` implementation: search `PATH` for an executable matching
/// `name`. Avoids pulling in the `which` crate for a single lookup.
fn find_on_path(name: &str) -> Option<PathBuf> {
    // An absolute or relative path is used as-is if it exists.
    let candidate = PathBuf::from(name);
    if candidate.is_absolute() || candidate.components().count() > 1 {
        if candidate.is_file() {
            return Some(candidate);
        }
        return None;
    }

    let path_env = std::env::var_os("PATH")?;
    for dir in std::env::split_paths(&path_env) {
        let full = dir.join(name);
        if is_executable(&full) {
            return Some(full);
        }
    }
    None
}

fn is_executable(path: &std::path::Path) -> bool {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let meta = match std::fs::metadata(path) {
            Ok(m) => m,
            Err(_) => return false,
        };
        if !meta.is_file() {
            return false;
        }
        meta.permissions().mode() & 0o111 != 0
    }
    #[cfg(not(unix))]
    {
        path.is_file()
    }
}

/// Build a `Command` pre-loaded with `<binary> native <sub>` and the JSON flag.
fn native_command(sub: &str, json: bool) -> Result<Command, String> {
    let binary = resolve_binary()?;
    let mut cmd = Command::new(binary);
    cmd.arg(NATIVE_SUBCOMMAND).arg(sub);
    if json {
        cmd.arg("--json");
    }
    Ok(cmd)
}

/// Run a `native <sub> --json` subcommand and return its stdout as bytes.
/// Captures stderr; on non-zero exit the stderr is returned as the error.
pub async fn run_native_json(app: &AppHandle, sub: &str) -> Result<Vec<u8>, String> {
    if std::env::var_os(BINARY_ENV).is_none() {
        if let Ok(sidecar) = app.shell().sidecar(BINARY_NAME) {
            let output = sidecar
                .args([NATIVE_SUBCOMMAND, sub, "--json"])
                .output()
                .await
                .map_err(|e| format!("failed to execute bundled binary: {e}"))?;
            if output.status.success() {
                return Ok(output.stdout);
            }
            return Err(command_error(sub, &output.stdout, &output.stderr));
        }
    }
    let mut cmd = native_command(sub, true)?;
    let output = cmd
        .output()
        .map_err(|e| format!("failed to execute: {e}"))?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
        let detail = if !stderr.is_empty() {
            stderr
        } else if !stdout.is_empty() {
            stdout
        } else {
            format!("exit code {}", output.status.code().unwrap_or(-1))
        };
        return Err(format!("`native {sub} --json` failed: {detail}"));
    }
    Ok(output.stdout)
}

/// Run `native <sub>` without JSON and return (ok, combined-message).
pub async fn run_native_plain(app: &AppHandle, sub: &str) -> (bool, String) {
    if std::env::var_os(BINARY_ENV).is_none() {
        if let Ok(sidecar) = app.shell().sidecar(BINARY_NAME) {
            match sidecar.args([NATIVE_SUBCOMMAND, sub]).output().await {
                Ok(output) => {
                    let message = output_message(&output.stdout, &output.stderr);
                    return (output.status.success(), message);
                }
                Err(error) => return (false, format!("failed to execute bundled binary: {error}")),
            }
        }
    }
    match native_command(sub, false) {
        Ok(mut cmd) => match cmd.output() {
            Ok(output) => {
                let msg = {
                    let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
                    let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
                    if !stderr.is_empty() {
                        stderr
                    } else if !stdout.is_empty() {
                        stdout
                    } else {
                        String::new()
                    }
                };
                (output.status.success(), msg)
            }
            Err(e) => (false, format!("failed to execute: {e}")),
        },
        Err(e) => (false, e),
    }
}

fn output_message(stdout: &[u8], stderr: &[u8]) -> String {
    let stderr = String::from_utf8_lossy(stderr).trim().to_string();
    let stdout = String::from_utf8_lossy(stdout).trim().to_string();
    if !stderr.is_empty() {
        stderr
    } else {
        stdout
    }
}

fn command_error(sub: &str, stdout: &[u8], stderr: &[u8]) -> String {
    let detail = output_message(stdout, stderr);
    if detail.is_empty() {
        format!("`native {sub}` failed")
    } else {
        format!("`native {sub}` failed: {detail}")
    }
}
