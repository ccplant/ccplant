package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	DistributionGitHubRelease = "github-release"
	manifestLimit             = int64(1 << 20)
	archiveLimit              = int64(256 << 20)
	binaryLimit               = int64(256 << 20)
	defaultManifestURL        = "https://github.com/ccplant/ccplant/releases/latest/download/ccplant-update-manifest.json"
)

var stableVersionPattern = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)

type Asset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Manifest struct {
	SchemaVersion int              `json:"schema_version"`
	Version       string           `json:"version"`
	PublishedAt   time.Time        `json:"published_at"`
	Assets        map[string]Asset `json:"assets"`
}

type State struct {
	SchemaVersion        int        `json:"schema_version"`
	Enabled              bool       `json:"enabled"`
	IntervalHours        int        `json:"interval_hours"`
	LastCheckedAt        *time.Time `json:"last_checked_at,omitempty"`
	LatestVersion        string     `json:"latest_version,omitempty"`
	LastInstalledVersion string     `json:"last_installed_version,omitempty"`
	LastErrorAt          *time.Time `json:"last_error_at,omitempty"`
	LastErrorCode        string     `json:"last_error_code,omitempty"`
}

type Result struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	Updated        bool   `json:"updated"`
}

type Options struct {
	CurrentVersion string
	Distribution   string
	PublicKey      string
	ManifestURL    string
	ReleaseBaseURL string
	Executable     string
	StateDir       string
	GOOS           string
	GOARCH         string
	HTTPClient     *http.Client
	Now            func() time.Time
	Probe          func(context.Context, string, string) error
}

type Updater struct{ options Options }

func New(options Options) *Updater {
	if options.ManifestURL == "" {
		options.ManifestURL = defaultManifestURL
	}
	if options.ReleaseBaseURL == "" {
		options.ReleaseBaseURL = "https://github.com/ccplant/ccplant/releases/download"
	}
	if options.GOOS == "" {
		options.GOOS = runtime.GOOS
	}
	if options.GOARCH == "" {
		options.GOARCH = runtime.GOARCH
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.HTTPClient == nil {
		options.HTTPClient = secureHTTPClient(2 * time.Minute)
	}
	if options.Probe == nil {
		options.Probe = probeVersion
	}
	return &Updater{options: options}
}

func (u *Updater) Eligible() bool {
	return u.options.Distribution == DistributionGitHubRelease && platformAssetName(u.options.GOOS, u.options.GOARCH) != "" && stableVersionPattern.MatchString(u.options.CurrentVersion) && u.options.PublicKey != ""
}

func (u *Updater) Check(ctx context.Context) (Result, error) {
	result := Result{CurrentVersion: u.options.CurrentVersion}
	manifest, _, err := u.fetchManifest(ctx)
	if err != nil {
		return result, err
	}
	result.LatestVersion = manifest.Version
	return result, nil
}

func (u *Updater) Apply(ctx context.Context) (Result, error) {
	result := Result{CurrentVersion: u.options.CurrentVersion}
	if !u.Eligible() {
		return result, errors.New("this ccplant distribution is not eligible for self-update")
	}
	unlock, locked, err := u.tryLock()
	if err != nil {
		return result, fmt.Errorf("acquire update lock: %w", err)
	}
	if !locked {
		return result, errors.New("another ccplant update is already running")
	}
	defer unlock()

	manifest, _, err := u.fetchManifest(ctx)
	if err != nil {
		return result, err
	}
	result.LatestVersion = manifest.Version
	comparison, err := compareStableVersions(u.options.CurrentVersion, manifest.Version)
	if err != nil {
		return result, err
	}
	if comparison >= 0 {
		return result, nil
	}
	asset, ok := manifest.Assets[u.options.GOOS+"/"+u.options.GOARCH]
	if !ok || asset.Name != platformAssetName(u.options.GOOS, u.options.GOARCH) {
		return result, errors.New("update manifest has no asset for this platform")
	}
	if asset.Size <= 0 || asset.Size > archiveLimit || !validSHA256(asset.SHA256) {
		return result, errors.New("update manifest contains invalid asset metadata")
	}
	archiveURL := strings.TrimRight(u.options.ReleaseBaseURL, "/") + "/" + url.PathEscape(manifest.Version) + "/" + url.PathEscape(asset.Name)
	archive, err := u.download(ctx, archiveURL, asset.Size)
	if err != nil {
		return result, fmt.Errorf("download update archive: %w", err)
	}
	sum := sha256.Sum256(archive)
	if !strings.EqualFold(asset.SHA256, hex.EncodeToString(sum[:])) {
		return result, errors.New("update archive checksum verification failed")
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return result, err
	}
	executable := u.options.Executable
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return result, fmt.Errorf("resolve executable: %w", err)
		}
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	if err := u.install(ctx, executable, binary, manifest.Version); err != nil {
		return result, err
	}
	result.Updated = true
	return result, nil
}

func (u *Updater) fetchManifest(ctx context.Context) (Manifest, []byte, error) {
	var manifest Manifest
	manifestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	data, err := u.download(manifestContext, u.options.ManifestURL, manifestLimit)
	if err != nil {
		return manifest, nil, fmt.Errorf("download update manifest: %w", err)
	}
	signature, err := u.download(manifestContext, u.options.ManifestURL+".sig", 1024)
	if err != nil {
		return manifest, nil, fmt.Errorf("download update manifest signature: %w", err)
	}
	publicKey, err := base64.StdEncoding.DecodeString(u.options.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return manifest, nil, errors.New("invalid embedded update public key")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), data, sig) {
		return manifest, nil, errors.New("update manifest signature verification failed")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, nil, fmt.Errorf("decode update manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 || !stableVersionPattern.MatchString(manifest.Version) {
		return manifest, nil, errors.New("unsupported update manifest")
	}
	return manifest, data, nil
}

func (u *Updater) download(ctx context.Context, target string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	response, err := u.options.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, errors.New("response exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("response exceeds size limit")
	}
	return data, nil
}

func (u *Updater) install(ctx context.Context, destination string, binary []byte, version string) error {
	directory := filepath.Dir(destination)
	temporary, err := os.CreateTemp(directory, ".ccplant-update-*")
	if err != nil {
		return fmt.Errorf("create update file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o755); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("prepare update file: %w", err)
	}
	if _, err := temporary.Write(binary); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write update file: %w", err)
	}
	err = temporary.Sync()
	closeErr := temporary.Close()
	if err != nil {
		return fmt.Errorf("write update file: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close update file: %w", closeErr)
	}
	if err := u.options.Probe(ctx, temporaryPath, version); err != nil {
		return fmt.Errorf("verify update binary: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("replace executable: %w", err)
	}
	if directoryHandle, openErr := os.Open(directory); openErr == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}

func (u *Updater) tryLock() (func(), bool, error) {
	directory, err := u.stateDirectory()
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, false, err
	}
	lock, err := os.OpenFile(filepath.Join(directory, "update.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return func() {}, false, nil
		}
		return nil, false, err
	}
	return func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }, true, nil
}

func (u *Updater) stateDirectory() (string, error) {
	if u.options.StateDir != "" {
		return u.options.StateDir, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "ccplant"), nil
}

func (u *Updater) LoadState() (State, error) {
	state := State{SchemaVersion: 1, Enabled: u.options.Distribution == DistributionGitHubRelease, IntervalHours: 24}
	directory, err := u.stateDirectory()
	if err != nil {
		return state, err
	}
	data, err := os.ReadFile(filepath.Join(directory, "update.json"))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil || state.SchemaVersion != 1 || state.IntervalHours <= 0 {
		return State{SchemaVersion: 1, Enabled: u.options.Distribution == DistributionGitHubRelease, IntervalHours: 24}, nil
	}
	return state, nil
}

func (u *Updater) SaveState(state State) error {
	directory, err := u.stateDirectory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".update-state-*")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer func() { _ = os.Remove(path) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	err = temporary.Sync()
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path, filepath.Join(directory, "update.json"))
}

func (u *Updater) SetEnabled(enabled bool) (State, error) {
	state, err := u.LoadState()
	if err != nil {
		return state, err
	}
	state.Enabled = enabled
	return state, u.SaveState(state)
}

func (u *Updater) AutoApply(ctx context.Context) (Result, error) {
	state, err := u.LoadState()
	if err != nil {
		return Result{}, err
	}
	enabled := state.Enabled
	if value, exists := os.LookupEnv("CCPLANT_AUTO_UPDATE"); exists {
		enabled = value == "1" || strings.EqualFold(value, "true")
	}
	if !enabled || !u.Eligible() {
		return Result{CurrentVersion: u.options.CurrentVersion}, nil
	}
	now := u.options.Now().UTC()
	if state.LastCheckedAt != nil && now.Before(state.LastCheckedAt.Add(time.Duration(state.IntervalHours)*time.Hour)) {
		return Result{CurrentVersion: u.options.CurrentVersion, LatestVersion: state.LatestVersion}, nil
	}
	result, applyErr := u.Apply(ctx)
	state.LastCheckedAt = &now
	state.LatestVersion = result.LatestVersion
	if applyErr != nil {
		state.LastErrorAt = &now
		state.LastErrorCode = classifyError(applyErr)
	} else {
		state.LastErrorAt = nil
		state.LastErrorCode = ""
		if result.Updated {
			state.LastInstalledVersion = result.LatestVersion
		}
	}
	if saveErr := u.SaveState(state); applyErr == nil && saveErr != nil {
		return result, saveErr
	}
	return result, applyErr
}

func secureHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "https" || !allowedHost(req.URL.Hostname()) {
			return errors.New("update redirect target is not allowed")
		}
		return nil
	}}
}

func allowedHost(host string) bool {
	return host == "github.com" || host == "objects.githubusercontent.com" || host == "release-assets.githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

func extractBinary(data []byte) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open update archive: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()
	tarReader := tar.NewReader(gzipReader)
	var binary []byte
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read update archive: %w", err)
		}
		if filepath.Base(header.Name) != "ccplant" {
			continue
		}
		if header.Name != "ccplant" || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > binaryLimit || binary != nil {
			return nil, errors.New("update archive contains an invalid ccplant entry")
		}
		binary, err = io.ReadAll(io.LimitReader(tarReader, binaryLimit+1))
		if err != nil || int64(len(binary)) != header.Size {
			return nil, errors.New("update archive contains a truncated ccplant binary")
		}
	}
	if binary == nil {
		return nil, errors.New("update archive does not contain ccplant")
	}
	return binary, nil
}

func probeVersion(ctx context.Context, binary, version string) error {
	probeContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeContext, binary, "--version").CombinedOutput()
	if err != nil {
		return err
	}
	if !strings.Contains(string(output), version) {
		return fmt.Errorf("binary reported %q, expected %s", strings.TrimSpace(string(output)), version)
	}
	return nil
}

func platformAssetName(goos, goarch string) string {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "ccplant_Linux_x86_64.tar.gz"
	case "linux/arm64":
		return "ccplant_Linux_arm64.tar.gz"
	case "darwin/arm64":
		return "ccplant_Darwin_arm64.tar.gz"
	default:
		return ""
	}
}

func compareStableVersions(left, right string) (int, error) {
	parse := func(value string) ([3]uint64, error) {
		var parsed [3]uint64
		match := stableVersionPattern.FindStringSubmatch(value)
		if match == nil {
			return parsed, fmt.Errorf("invalid stable version %q", value)
		}
		for index := range parsed {
			number, err := strconv.ParseUint(match[index+1], 10, 64)
			if err != nil {
				return parsed, fmt.Errorf("invalid stable version %q", value)
			}
			parsed[index] = number
		}
		return parsed, nil
	}
	a, err := parse(left)
	if err != nil {
		return 0, err
	}
	b, err := parse(right)
	if err != nil {
		return 0, err
	}
	for index := range a {
		if a[index] < b[index] {
			return -1, nil
		}
		if a[index] > b[index] {
			return 1, nil
		}
	}
	return 0, nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func classifyError(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "permission denied"):
		return "permission_denied"
	case strings.Contains(message, "signature"):
		return "signature_invalid"
	case strings.Contains(message, "checksum"):
		return "checksum_invalid"
	case strings.Contains(message, "HTTP"), strings.Contains(message, "download"):
		return "network_error"
	default:
		return "update_failed"
	}
}
