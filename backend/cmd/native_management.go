package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
)

var nativeManagementExit = os.Exit
var nativeManagementStartedAt = time.Now().UTC()
var nativeReleaseVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

func registerNativeManagementRoutes(e *echo.Echo, options struct {
	listen, upstreamURL, connectionToken, upstreamAuthToken, stateDir, binaryPath, managerID, configPath string
	filesystemSandbox                                                                                    bool
	inheritRuntimeProfile                                                                                bool
}, manager *services.NativeSessionManager, secret []byte) {
	group := e.Group("/internal/esm-management", nativeManagementAuth(secret))
	group.GET("/status", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"status": "online", "version": nativeBuildVersion(), "active_sessions": manager.ActiveSessionCount(),
			"uptime_seconds": int64(time.Since(nativeManagementStartedAt).Seconds()),
			"capabilities":   []string{"status", "logs", "restart", "upgrade"},
		})
	})
	group.GET("/logs", func(c echo.Context) error {
		tail, _ := strconv.Atoi(c.QueryParam("tail"))
		if tail < 1 || tail > 5000 {
			tail = 200
		}
		logPath := nativeDaemonLogPath(options.configPath)
		lines, err := tailFile(logPath, tail, 2<<20)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return c.JSON(http.StatusOK, map[string]interface{}{"lines": []string{}, "source": logPath})
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to read daemon logs").SetInternal(err)
		}
		return c.JSON(http.StatusOK, map[string]interface{}{"lines": lines, "source": logPath})
	})
	group.POST("/restart", func(c echo.Context) error {
		go func() {
			time.Sleep(250 * time.Millisecond)
			nativeManagementExit(0) // systemd/launchd KeepAlive starts the managed daemon again.
		}()
		return c.JSON(http.StatusAccepted, map[string]string{"status": "restarting"})
	})
	group.POST("/upgrade", func(c echo.Context) error {
		var request struct {
			Version string `json:"version"`
		}
		if err := c.Bind(&request); err != nil || !validReleaseVersion(request.Version) {
			return echo.NewHTTPError(http.StatusBadRequest, "version must be a release such as v1.20.0")
		}
		if manager.ActiveSessionCount() > 0 {
			return echo.NewHTTPError(http.StatusConflict, "drain active sessions before upgrading")
		}
		if err := installNativeRelease(c.Request().Context(), request.Version); err != nil {
			return echo.NewHTTPError(http.StatusBadGateway, "failed to install release").SetInternal(err)
		}
		go func() {
			time.Sleep(250 * time.Millisecond)
			nativeManagementExit(0)
		}()
		return c.JSON(http.StatusAccepted, map[string]string{"status": "upgrading", "version": request.Version})
	})
}

func nativeManagementAuth(secret []byte) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			body, err := io.ReadAll(io.LimitReader(c.Request().Body, 64*1024))
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
			}
			c.Request().Body = io.NopCloser(strings.NewReader(string(body)))
			ts := c.Request().Header.Get(hmacutil.TimestampHeader)
			msg := hmacutil.BuildMessage(c.Request().Method, c.Request().URL.RequestURI(), ts, body)
			if hmacutil.ValidateTimestamp(ts) != nil || !hmacutil.Verify(secret, msg, c.Request().Header.Get("X-Hub-Signature-256")) {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid signature")
			}
			return next(c)
		}
	}
}

func nativeDaemonLogPath(configPath string) string {
	if configPath != "" {
		if strings.Contains(configPath, "Library/Application Support") {
			return filepath.Join(filepath.Dir(configPath), "logs", "native.log")
		}
		if base := filepath.Base(filepath.Dir(configPath)); strings.HasPrefix(base, "agentapi-native-") {
			return filepath.Join("/var/log", base, "native.log")
		}
	}
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "agentapi-native", "logs", "native.log")
	}
	return "/var/log/agentapi-native/native.log"
}

func tailFile(path string, count, maxBytes int) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return lines, nil
}

func validReleaseVersion(version string) bool {
	return len(version) <= 64 && nativeReleaseVersionPattern.MatchString(version)
}

func installNativeRelease(ctx context.Context, version string) error {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	osName := map[string]string{"linux": "Linux", "darwin": "Darwin"}[runtime.GOOS]
	if osName == "" {
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
	asset := fmt.Sprintf("ccplant_%s_%s.tar.gz", osName, arch)
	base := "https://github.com/ccplant/ccplant/releases/download/" + version + "/"
	archive, err := downloadNativeAsset(ctx, base+asset, 256<<20)
	if err != nil {
		return err
	}
	checksums, err := downloadNativeAsset(ctx, base+"checksums.txt", 2<<20)
	if err != nil {
		return err
	}
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			want = fields[0]
		}
	}
	sum := sha256.Sum256(archive)
	if want == "" || !strings.EqualFold(want, hex.EncodeToString(sum[:])) {
		return errors.New("release checksum verification failed")
	}
	binary, err := extractNativeBinary(archive)
	if err != nil {
		return err
	}
	destination, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(destination); resolveErr == nil {
		destination = resolved
	}
	return atomicWriteFile(destination, binary, 0o755)
}

func downloadNativeAsset(ctx context.Context, target string, limit int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", filepath.Base(target), resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func extractNativeBinary(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tarReader := tar.NewReader(gz)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(header.Name) == "ccplant" && header.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tarReader, 256<<20))
		}
	}
	return nil, errors.New("release archive does not contain ccplant")
}
