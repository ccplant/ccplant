package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
)

var antigravityAuthRequestFile string

var AntigravityAuthWorkerCmd = &cobra.Command{
	Use: "antigravity-auth-worker", Short: "Run an isolated Antigravity Business SSO attempt", Hidden: true,
	Args: cobra.NoArgs, RunE: runAntigravityAuthWorker,
}

func init() {
	AntigravityAuthWorkerCmd.Flags().StringVar(&antigravityAuthRequestFile, "request-file", "", "path to the authentication request")
	_ = AntigravityAuthWorkerCmd.MarkFlagRequired("request-file")
}

type antigravityInput struct {
	Input   string `json:"input"`
	Version int64  `json:"version"`
}

const antigravitySubscriptionPrefix = "\x00subscription:"

type antigravityAuthBundle struct {
	Version int                         `json:"version"`
	Files   []antigravityAuthBundleFile `json:"files"`
}

type antigravityAuthBundleFile struct {
	Path    string `json:"path"`
	Content string `json:"content_base64"`
}

func runAntigravityAuthWorker(_ *cobra.Command, _ []string) error {
	if err := configureAntigravityHerdr(); err != nil {
		return fmt.Errorf("configure antigravity terminal: %w", err)
	}
	data, err := os.ReadFile(antigravityAuthRequestFile)
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
	result, runErr := executeAntigravityAuth(ctx, request)
	if runErr != nil {
		log.Printf("[ANTIGRAVITY_AUTH_WORKER] Attempt %s failed: %v", request.AttemptID, runErr)
		result = codexauth.Result{Status: codexauth.StatusFailed, ErrorCode: "antigravity_login_failed", ErrorMessage: boundedAntigravityError(runErr)}
	}
	reportCtx, reportCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer reportCancel()
	if err := postCodexAuthJSON(reportCtx, request, "result", result); err != nil {
		return fmt.Errorf("report result: %w", err)
	}
	return runErr
}

func configureAntigravityHerdr() error {
	// Authentication workers run with a read-only root filesystem. herdr-core
	// normally writes below $HOME/.local/state, so keep its isolated state and
	// socket below the worker's writable /tmp volume instead.
	root := filepath.Join(os.TempDir(), "ccplant-antigravity-herdr")
	stateDir := filepath.Join(root, "state")
	runtimeDir := filepath.Join(root, "runtime")
	for _, dir := range []string{stateDir, runtimeDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	if err := os.Setenv("XDG_STATE_HOME", stateDir); err != nil {
		return err
	}
	return os.Setenv("XDG_RUNTIME_DIR", runtimeDir)
}

func boundedAntigravityError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	const limit = 2048
	if len(message) > limit {
		message = message[:limit]
	}
	return message
}

func executeAntigravityAuth(ctx context.Context, request codexauth.WorkloadRequest) (codexauth.Result, error) {
	path, err := exec.LookPath("agy")
	if err != nil {
		return codexauth.Result{}, errors.New("antigravity CLI is unavailable")
	}
	terminal, err := startAntigravityTerminal(ctx, path, request.AttemptID)
	if err != nil {
		return codexauth.Result{}, err
	}
	defer cleanupAntigravityTerminal(terminal)

	var verificationURI string
	var inputVersion int64
	suppressTranscript := false
	poll := time.NewTicker(500 * time.Millisecond)
	defer poll.Stop()
	var pendingBundle []byte
	var subscriptionTranscript string
	var lastChallengeTranscript string
	subscriptionSelected := false
	for {
		select {
		case <-poll.C:
			transcript, finished, exitCode, err := readAntigravityTerminal(ctx, terminal)
			if err != nil {
				return codexauth.Result{}, err
			}
			clean := codexAuthANSIRegex.ReplaceAllString(transcript, "")
			if verificationURI == "" {
				verificationURI = codexAuthURLRegex.FindString(clean)
			}
			if strings.Contains(clean, "Select available subscription") {
				suppressTranscript = false
				subscriptionTranscript = clean
			}
			challengeTranscript := clean
			if finished && !subscriptionSelected && subscriptionTranscript != "" {
				challengeTranscript = subscriptionTranscript
			}
			if verificationURI != "" && !suppressTranscript && challengeTranscript != lastChallengeTranscript {
				challenge := codexauth.Challenge{VerificationURI: verificationURI, Transcript: challengeTranscript}
				_ = postCodexAuthJSON(ctx, request, "challenge", challenge)
				lastChallengeTranscript = challengeTranscript
			}
			if verificationURI != "" {
				input, err := fetchAntigravityInput(ctx, request, inputVersion)
				if err == nil && input.Version > inputVersion {
					inputVersion = input.Version
					isSubscription := strings.HasPrefix(input.Input, antigravitySubscriptionPrefix)
					suppressTranscript = !isSubscription
					if isSubscription {
						subscriptionSelected = true
					}
					// Agy may automatically finish when only one subscription is
					// available. In that case the UI selection is an explicit
					// confirmation; there is no live terminal left to receive keys.
					if !finished {
						if err := sendAntigravityTerminalInput(ctx, terminal, input.Input); err != nil {
							return codexauth.Result{}, err
						}
					} else if !isSubscription {
						return codexauth.Result{}, errors.New("antigravity login finished before authentication input was submitted")
					}
				}
			}
			if finished {
				if exitCode != 0 {
					return codexauth.Result{Status: codexauth.StatusDenied, ErrorCode: "authorization_denied"}, fmt.Errorf("antigravity login exited with status %d", exitCode)
				}
				if pendingBundle == nil {
					pendingBundle, err = collectAntigravityAuthBundle()
					if err != nil {
						return codexauth.Result{}, err
					}
				}
				if !subscriptionSelected {
					continue
				}
				return codexauth.Result{Status: codexauth.StatusAuthorized, AuthJSON: pendingBundle}, nil
			}
		case <-ctx.Done():
			return codexauth.Result{}, ctx.Err()
		}
	}
}

type hcRunResult struct {
	Address string `json:"address"`
}

type hcReadResult struct {
	Text string `json:"text"`
}

type hcTerminalResult struct {
	State    string `json:"state"`
	ExitCode *int   `json:"exit_code"`
}

func startAntigravityTerminal(ctx context.Context, agyPath, attemptID string) (string, error) {
	nameSuffix := strings.TrimPrefix(attemptID, "aga-")
	if len(nameSuffix) > 12 {
		nameSuffix = nameSuffix[:12]
	}
	if nameSuffix == "" {
		return "", errors.New("antigravity attempt ID is required")
	}
	name := "agy-auth-" + nameSuffix
	var result hcRunResult
	// hc supplies the PTY used by agy's interactive subscription picker. Pass
	// SSH_CONNECTION through env(1) because hc terminals inherit the core's
	// environment, not the environment of this client process.
	if err := runHCJSON(ctx, &result, "run", "--bg", "--no-parent", "--no-notify", "--name", name, "--json", "env", "SSH_CONNECTION=127.0.0.1 65535 127.0.0.1 22", agyPath, "acp", "--login"); err != nil {
		return "", fmt.Errorf("start antigravity terminal: %w", err)
	}
	if result.Address == "" {
		return "", errors.New("hc did not return an antigravity terminal address")
	}
	return result.Address, nil
}

func readAntigravityTerminal(ctx context.Context, terminal string) (string, bool, int, error) {
	var screen hcReadResult
	if err := runHCJSON(ctx, &screen, "read", terminal, "--json", "--lines", "0"); err != nil {
		return "", false, 0, fmt.Errorf("read antigravity terminal: %w", err)
	}
	var state hcTerminalResult
	if err := runHCJSON(ctx, &state, "get", terminal, "--json"); err != nil {
		return "", false, 0, fmt.Errorf("inspect antigravity terminal: %w", err)
	}
	exitCode := 0
	if state.ExitCode != nil {
		exitCode = *state.ExitCode
	}
	return screen.Text, state.State == "finished", exitCode, nil
}

func sendAntigravityTerminalInput(ctx context.Context, terminal, input string) error {
	if strings.HasPrefix(input, antigravitySubscriptionPrefix) {
		index, err := strconv.Atoi(strings.TrimPrefix(input, antigravitySubscriptionPrefix))
		if err != nil || index < 0 || index > 100 {
			return errors.New("invalid antigravity subscription index")
		}
		keys := []string{"keys", terminal}
		for range index {
			keys = append(keys, "down")
		}
		keys = append(keys, "enter", "--json")
		return runHCJSON(ctx, nil, keys...)
	}
	return sendHCText(ctx, terminal, input)
}

func sendHCText(ctx context.Context, terminal, input string) error {
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "terminal.send",
		"params":  map[string]string{"terminal": terminal, "text": input},
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "hc", "connect")
	cmd.Stdin = strings.NewReader(string(payload) + "\n")
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	var response struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return err
	}
	if len(response.Error) != 0 && string(response.Error) != "null" {
		return errors.New("hc rejected antigravity terminal input")
	}
	return nil
}

func cleanupAntigravityTerminal(terminal string) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = runHCJSON(cleanupCtx, nil, "kill", terminal, "--json")
	_ = runHCJSON(cleanupCtx, nil, "rm", terminal, "--json")
}

func runHCJSON(ctx context.Context, value any, args ...string) error {
	cmd := exec.CommandContext(ctx, "hc", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	if value == nil {
		return nil
	}
	return json.Unmarshal(output, value)
}

func fetchAntigravityInput(ctx context.Context, request codexauth.WorkloadRequest, after int64) (antigravityInput, error) {
	endpoint := fmt.Sprintf("%s/%s/input?after=%d", strings.TrimRight(request.CallbackURL, "/"), request.AttemptID, after)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return antigravityInput{}, err
	}
	req.Header.Set("Authorization", "Bearer "+request.Token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return antigravityInput{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return antigravityInput{}, fmt.Errorf("input callback returned %d", resp.StatusCode)
	}
	var result antigravityInput
	return result, json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&result)
}

func collectAntigravityAuthBundle() ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, ".gemini", "antigravity-cli")
	var files []antigravityAuthBundleFile
	total := 0
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if path != root && (info.Name() == "log" || info.Name() == "conversations" || info.Name() == "cache") {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() || strings.HasSuffix(info.Name(), ".db") || strings.Contains(info.Name(), ".db-") {
			return nil
		}
		if info.Size() > 128<<10 || total+int(info.Size()) > 512<<10 {
			return fmt.Errorf("Antigravity auth bundle is too large")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return errors.New("invalid Antigravity auth path")
		}
		total += len(content)
		files = append(files, antigravityAuthBundleFile{Path: filepath.ToSlash(rel), Content: base64.StdEncoding.EncodeToString(content)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("antigravity did not create portable authentication files")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return json.Marshal(antigravityAuthBundle{Version: 1, Files: files})
}
