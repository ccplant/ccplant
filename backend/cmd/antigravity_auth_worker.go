package cmd

import (
	"bufio"
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

type antigravityAuthBundle struct {
	Version int                         `json:"version"`
	Files   []antigravityAuthBundleFile `json:"files"`
}

type antigravityAuthBundleFile struct {
	Path    string `json:"path"`
	Content string `json:"content_base64"`
}

func runAntigravityAuthWorker(_ *cobra.Command, _ []string) error {
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
	cmd := exec.CommandContext(ctx, path, "acp", "--login")
	cmd.Env = append(os.Environ(), "SSH_CONNECTION=ccplant-auth-workload")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return codexauth.Result{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return codexauth.Result{}, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return codexauth.Result{}, err
	}

	lines := make(chan string, 8)
	scanErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
		scanErr <- scanner.Err()
	}()

	var transcript strings.Builder
	var verificationURI string
	var inputVersion int64
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	for lines != nil {
		select {
		case line, ok := <-lines:
			if !ok {
				lines = nil
				continue
			}
			clean := codexAuthANSIRegex.ReplaceAllString(line, "")
			if transcript.Len() < 16<<10 {
				transcript.WriteString(clean)
				transcript.WriteByte('\n')
			}
			if verificationURI == "" {
				verificationURI = codexAuthURLRegex.FindString(clean)
			}
			if verificationURI != "" {
				challenge := codexauth.Challenge{VerificationURI: verificationURI, Transcript: transcript.String()}
				_ = postCodexAuthJSON(ctx, request, "challenge", challenge)
			}
		case <-poll.C:
			if verificationURI == "" {
				continue
			}
			input, err := fetchAntigravityInput(ctx, request, inputVersion)
			if err == nil && input.Version > inputVersion {
				inputVersion = input.Version
				if _, err := io.WriteString(stdin, input.Input+"\n"); err != nil {
					return codexauth.Result{}, err
				}
			}
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return codexauth.Result{}, ctx.Err()
		}
	}
	if err := <-scanErr; err != nil {
		return codexauth.Result{}, err
	}
	if err := cmd.Wait(); err != nil {
		return codexauth.Result{Status: codexauth.StatusDenied, ErrorCode: "authorization_denied"}, err
	}
	bundle, err := collectAntigravityAuthBundle()
	if err != nil {
		return codexauth.Result{}, err
	}
	return codexauth.Result{Status: codexauth.StatusAuthorized, AuthJSON: bundle}, nil
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
