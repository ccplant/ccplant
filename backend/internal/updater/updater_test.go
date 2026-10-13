package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplyVerifiesAndAtomicallyReplacesBinary(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := testArchive(t, "new binary")
	sum := sha256.Sum256(archive)
	manifest := Manifest{SchemaVersion: 1, Version: "v1.2.0", PublishedAt: time.Now().UTC(), Assets: map[string]Asset{
		"linux/amd64": {Name: "ccplant_Linux_x86_64.tar.gz", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(archive))},
	}}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestData))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/manifest.json":
			_, _ = response.Write(manifestData)
		case request.URL.Path == "/manifest.json.sig":
			_, _ = io.WriteString(response, signature)
		case strings.HasSuffix(request.URL.Path, "/ccplant_Linux_x86_64.tar.gz"):
			_, _ = response.Write(archive)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	directory := t.TempDir()
	executable := filepath.Join(directory, "ccplant")
	if err := os.WriteFile(executable, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	service := New(Options{
		CurrentVersion: "v1.1.0", Distribution: DistributionGitHubRelease,
		PublicKey: base64.StdEncoding.EncodeToString(publicKey), ManifestURL: server.URL + "/manifest.json",
		ReleaseBaseURL: server.URL, Executable: executable, StateDir: filepath.Join(directory, "state"),
		GOOS: "linux", GOARCH: "amd64", HTTPClient: server.Client(),
		Probe: func(_ context.Context, path, version string) error {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if string(data) != "new binary" || version != "v1.2.0" {
				t.Fatalf("unexpected probe input %q, %q", data, version)
			}
			return nil
		},
	})
	result, err := service.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.LatestVersion != "v1.2.0" {
		t.Fatalf("unexpected result: %+v", result)
	}
	installed, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != "new binary" {
		t.Fatalf("installed binary = %q", installed)
	}
}

func TestApplyRejectsTamperedManifestWithoutReplacingBinary(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifestData := []byte(`{"schema_version":1,"version":"v1.2.0","published_at":"2026-10-13T00:00:00Z","assets":{}}`)
	signature := ed25519.Sign(privateKey, manifestData)
	manifestData = bytes.Replace(manifestData, []byte("v1.2.0"), []byte("v1.3.0"), 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, ".sig") {
			_, _ = io.WriteString(response, base64.StdEncoding.EncodeToString(signature))
			return
		}
		_, _ = response.Write(manifestData)
	}))
	defer server.Close()
	directory := t.TempDir()
	executable := filepath.Join(directory, "ccplant")
	if err := os.WriteFile(executable, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	service := New(Options{CurrentVersion: "v1.1.0", Distribution: DistributionGitHubRelease, PublicKey: base64.StdEncoding.EncodeToString(publicKey), ManifestURL: server.URL, Executable: executable, StateDir: filepath.Join(directory, "state"), GOOS: "linux", GOARCH: "amd64", HTTPClient: server.Client()})
	if _, err := service.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("expected signature error, got %v", err)
	}
	installed, err := os.ReadFile(executable)
	if err != nil || string(installed) != "old binary" {
		t.Fatalf("existing binary changed: %q, %v", installed, err)
	}
}

func TestStateAndEligibility(t *testing.T) {
	directory := t.TempDir()
	service := New(Options{CurrentVersion: "v1.0.0", Distribution: DistributionGitHubRelease, PublicKey: base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize)), StateDir: directory, GOOS: "linux", GOARCH: "amd64"})
	if !service.Eligible() {
		t.Fatal("release binary should be eligible")
	}
	state, err := service.LoadState()
	if err != nil || !state.Enabled || state.IntervalHours != 24 {
		t.Fatalf("unexpected default state: %+v, %v", state, err)
	}
	state.Enabled = false
	state.LatestVersion = "v1.2.3"
	if err := service.SaveState(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.LoadState()
	if err != nil || loaded.Enabled || loaded.LatestVersion != "v1.2.3" {
		t.Fatalf("unexpected stored state: %+v, %v", loaded, err)
	}
	container := New(Options{CurrentVersion: "v1.0.0", Distribution: "container", PublicKey: base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize)), GOOS: "linux", GOARCH: "amd64"})
	if container.Eligible() {
		t.Fatal("container binary must not be eligible")
	}
}

func TestExtractBinaryRejectsUnsafeEntry(t *testing.T) {
	archive := testArchiveNamed(t, "../ccplant", "bad")
	if _, err := extractBinary(archive); err == nil {
		t.Fatal("expected unsafe archive entry to be rejected")
	}
}

func TestCompareStableVersions(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{{"v1.2.3", "v1.2.4", -1}, {"v2.0.0", "v1.99.99", 1}, {"v1.2.3", "v1.2.3", 0}}
	for _, test := range cases {
		got, err := compareStableVersions(test.left, test.right)
		if err != nil || got != test.want {
			t.Errorf("compareStableVersions(%q, %q) = %d, %v", test.left, test.right, got, err)
		}
	}
	if _, err := compareStableVersions("dev", "v1.0.0"); err == nil {
		t.Fatal("expected development version to be rejected")
	}
}

func testArchive(t *testing.T, content string) []byte {
	t.Helper()
	return testArchiveNamed(t, "ccplant", content)
}

func testArchiveNamed(t *testing.T, name, content string) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tarWriter, content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
