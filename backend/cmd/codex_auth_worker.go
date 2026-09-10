package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
)

var (
	codexAuthANSIRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	codexAuthURLRegex  = regexp.MustCompile(`https://[^\s]+`)
	codexAuthCodeRegex = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4,8}\b`)
)

var CodexAuthWorkerCmd = &cobra.Command{
	Use:    "codex-auth-worker",
	Short:  "Run an isolated Codex device authentication attempt",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runCodexAuthWorker,
}

var codexAuthRequestFile string

func init() {
	CodexAuthWorkerCmd.Flags().StringVar(&codexAuthRequestFile, "request-file", "", "path to the authentication request")
	_ = CodexAuthWorkerCmd.MarkFlagRequired("request-file")
}

func runCodexAuthWorker(_ *cobra.Command, _ []string) error {
	data, err := os.ReadFile(codexAuthRequestFile)
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	var request codexauth.WorkloadRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if request.AttemptID == "" || request.CallbackURL == "" || request.Token == "" || request.ExpiresAt.IsZero() {
		return errors.New("invalid authentication request")
	}
	ctx, cancel := context.WithDeadline(context.Background(), request.ExpiresAt)
	defer cancel()
	result, err := executeCodexDeviceAuth(ctx, request)
	if err != nil {
		result = codexauth.Result{Status: codexauth.StatusFailed, ErrorCode: "codex_login_failed"}
	}
	reportCtx, reportCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer reportCancel()
	if postErr := postCodexAuthJSON(reportCtx, request, "result", result); postErr != nil {
		return fmt.Errorf("report result: %w", postErr)
	}
	return err
}

func executeCodexDeviceAuth(ctx context.Context, request codexauth.WorkloadRequest) (codexauth.Result, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return codexauth.Result{}, errors.New("codex CLI is unavailable")
	}
	log.Printf("[CODEX_AUTH_WORKER] Starting Codex device login (executable=%q)", path)
	cmd := exec.CommandContext(ctx, path, "login", "--device-auth")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return codexauth.Result{}, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return codexauth.Result{}, err
	}
	log.Printf("[CODEX_AUTH_WORKER] Codex device login started (pid=%d)", cmd.Process.Pid)
	challengeCh := make(chan codexauth.Challenge, 1)
	parseErrCh := make(chan error, 1)
	go parseCodexAuthChallenge(pipe, challengeCh, parseErrCh)
	select {
	case challenge := <-challengeCh:
		log.Printf("[CODEX_AUTH_WORKER] Device challenge parsed; reporting callback")
		versionCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		version, _ := exec.CommandContext(versionCtx, path, "--version").Output()
		cancel()
		challenge.CLIVersion = strings.TrimSpace(string(version))
		if err := postCodexAuthJSON(ctx, request, "challenge", challenge); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return codexauth.Result{}, err
		}
		log.Printf("[CODEX_AUTH_WORKER] Device challenge callback accepted")
	case err := <-parseErrCh:
		log.Printf("[CODEX_AUTH_WORKER] Device challenge parser failed: %v", err)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return codexauth.Result{}, err
	case <-ctx.Done():
		log.Printf("[CODEX_AUTH_WORKER] Timed out waiting for device challenge: %v", ctx.Err())
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return codexauth.Result{}, ctx.Err()
	}
	if err := cmd.Wait(); err != nil {
		return codexauth.Result{Status: codexauth.StatusDenied, ErrorCode: "authorization_denied"}, err
	}
	status := exec.CommandContext(ctx, path, "login", "status")
	if err := status.Run(); err != nil {
		return codexauth.Result{}, errors.New("Codex login status did not confirm authentication")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return codexauth.Result{}, err
	}
	authJSON, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		return codexauth.Result{}, errors.New("Codex did not create auth.json")
	}
	if len(authJSON) == 0 || len(authJSON) > 32<<10 {
		return codexauth.Result{}, errors.New("invalid auth.json size")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(authJSON, &object) != nil || object == nil {
		return codexauth.Result{}, errors.New("invalid auth.json")
	}
	return codexauth.Result{Status: codexauth.StatusAuthorized, AuthJSON: authJSON}, nil
}

func parseCodexAuthChallenge(reader io.Reader, result chan<- codexauth.Challenge, errs chan<- error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 16<<10)
	var code, uri string
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		rawLine := scanner.Text()
		line := codexAuthANSIRegex.ReplaceAllString(rawLine, "")
		if uri == "" {
			uri = codexAuthURLRegex.FindString(line)
		}
		if code == "" {
			code = codexAuthCodeRegex.FindString(line)
		}
		log.Printf("[CODEX_AUTH_WORKER] Read CLI output line (line=%d bytes=%d stripped_bytes=%d uri_seen=%t code_seen=%t)", lineNumber, len(rawLine), len(line), uri != "", code != "")
		if code != "" && uri != "" {
			log.Printf("[CODEX_AUTH_WORKER] Device challenge fields found (line=%d)", lineNumber)
			result <- codexauth.Challenge{UserCode: code, VerificationURI: uri}
			_, _ = io.Copy(io.Discard, reader)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[CODEX_AUTH_WORKER] CLI output scan failed after %d lines: %v", lineNumber, err)
		errs <- err
		return
	}
	log.Printf("[CODEX_AUTH_WORKER] CLI output ended after %d lines without a complete challenge (uri_seen=%t code_seen=%t)", lineNumber, uri != "", code != "")
	errs <- errors.New("Codex login ended before returning a device challenge")
}

func postCodexAuthJSON(ctx context.Context, request codexauth.WorkloadRequest, suffix string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(request.CallbackURL, "/") + "/" + request.AttemptID + "/" + suffix
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+request.Token)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(httpRequest)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("callback returned %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	return nil
}
