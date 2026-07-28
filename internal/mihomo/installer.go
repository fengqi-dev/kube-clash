package mihomo

import (
	"archive/zip"
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
	"runtime"
	"strings"
	"time"
)

const downloadBaseURL = ProjectURL + "/releases/download/" + Version

type releaseAsset struct {
	Name   string
	SHA256 string
}

var releaseAssets = map[string]releaseAsset{
	"darwin/amd64": {
		Name:   "mihomo-darwin-amd64-compatible-v1.19.28.gz",
		SHA256: "a469cc2f6800e71b50eca3f74bc72a8f6f7e990a5d4aaecb81a68cf331516d9d",
	},
	"darwin/arm64": {
		Name:   "mihomo-darwin-arm64-v1.19.28.gz",
		SHA256: "40cdae2fab4b18df15f40eaa9dc3af70ab3d8be7f77164ae1e5f1af3a2a4fb44",
	},
	"linux/amd64": {
		Name:   "mihomo-linux-amd64-compatible-v1.19.28.gz",
		SHA256: "70d01cfb8cb7bf7a92fd1af16cb4b9553d90bb4eecde3b5c4849103e27c80ddb",
	},
	"linux/arm64": {
		Name:   "mihomo-linux-arm64-v1.19.28.gz",
		SHA256: "2474450cd1c41dfa53036a54a4e85579f493d3af524d86c3d4b8e2b240b56cd2",
	},
	"windows/amd64": {
		Name:   "mihomo-windows-amd64-compatible-v1.19.28.zip",
		SHA256: "6d8a079d01b3631e73e56b7b42a067afc14f9e3ad99f2880d38bb141cf8fcbe7",
	},
	"windows/arm64": {
		Name:   "mihomo-windows-arm64-v1.19.28.zip",
		SHA256: "25cedfb999864e834a3d8424cb8ea61b9145b3cb3aea0180b9fdc009623abeda",
	},
}

type Installer struct {
	HTTPClient  *http.Client
	BaseDir     string
	GOOS        string
	GOARCH      string
	Asset       *releaseAsset
	DownloadURL func(releaseAsset) string
}

func (i *Installer) Ensure(ctx context.Context) (string, error) {
	if override := os.Getenv("KUBE_CLASH_MIHOMO_PATH"); override != "" {
		return validateBinary(override)
	}
	goos, goarch := i.platform()
	asset, ok := releaseAssets[goos+"/"+goarch]
	if i.Asset != nil {
		asset, ok = *i.Asset, true
	}
	if !ok {
		return "", fmt.Errorf("mihomo %s is not available for %s/%s", Version, goos, goarch)
	}
	baseDir, err := i.baseDir()
	if err != nil {
		return "", err
	}
	binaryName := "mihomo-" + Version
	if goos == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(baseDir, "cores", binaryName)
	if path, validateErr := validateBinary(binaryPath); validateErr == nil {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		return "", fmt.Errorf("create mihomo core directory: %w", err)
	}

	client := i.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	url := downloadBaseURL + "/" + asset.Name
	if i.DownloadURL != nil {
		url = i.DownloadURL(asset)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create mihomo download request: %w", err)
	}
	request.Header.Set("User-Agent", "kube-clash/0.1")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download mihomo: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download mihomo: unexpected HTTP status %s", response.Status)
	}
	archive, err := io.ReadAll(io.LimitReader(response.Body, 128<<20))
	if err != nil {
		return "", fmt.Errorf("read mihomo download: %w", err)
	}
	if err := verifySHA256(archive, asset.SHA256); err != nil {
		return "", err
	}
	executable, err := extractExecutable(asset.Name, archive)
	if err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(filepath.Dir(binaryPath), ".mihomo-*")
	if err != nil {
		return "", fmt.Errorf("create temporary mihomo binary: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o755); err != nil {
		temp.Close()
		return "", fmt.Errorf("set mihomo permissions: %w", err)
	}
	if _, err := temp.Write(executable); err != nil {
		temp.Close()
		return "", fmt.Errorf("write mihomo binary: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return "", fmt.Errorf("sync mihomo binary: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close mihomo binary: %w", err)
	}
	if err := os.Rename(tempPath, binaryPath); err != nil {
		return "", fmt.Errorf("install mihomo binary: %w", err)
	}
	return validateBinary(binaryPath)
}

func (i *Installer) platform() (string, string) {
	goos, goarch := i.GOOS, i.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return goos, goarch
}

func (i *Installer) baseDir() (string, error) {
	if i.BaseDir != "" {
		return i.BaseDir, nil
	}
	value, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(value, "Kube Clash"), nil
}

func validateBinary(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("find mihomo binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("mihomo binary is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("mihomo binary is not executable")
	}
	return filepath.Clean(path), nil
}

func verifySHA256(content []byte, expected string) error {
	sum := sha256.Sum256(content)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("mihomo SHA-256 mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func extractExecutable(name string, content []byte) ([]byte, error) {
	switch {
	case strings.HasSuffix(name, ".gz"):
		reader, err := gzip.NewReader(bytes.NewReader(content))
		if err != nil {
			return nil, fmt.Errorf("open mihomo gzip archive: %w", err)
		}
		defer reader.Close()
		value, err := io.ReadAll(io.LimitReader(reader, 128<<20))
		if err != nil {
			return nil, fmt.Errorf("extract mihomo gzip archive: %w", err)
		}
		return value, nil
	case strings.HasSuffix(name, ".zip"):
		reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
		if err != nil {
			return nil, fmt.Errorf("open mihomo zip archive: %w", err)
		}
		for _, file := range reader.File {
			if file.FileInfo().Mode().IsRegular() &&
				strings.HasSuffix(strings.ToLower(file.Name), ".exe") {
				opened, openErr := file.Open()
				if openErr != nil {
					return nil, fmt.Errorf("open mihomo executable: %w", openErr)
				}
				value, readErr := io.ReadAll(io.LimitReader(opened, 128<<20))
				opened.Close()
				if readErr != nil {
					return nil, fmt.Errorf("extract mihomo executable: %w", readErr)
				}
				return value, nil
			}
		}
		return nil, errors.New("mihomo zip archive does not contain an executable")
	default:
		return nil, fmt.Errorf("unsupported mihomo archive %q", name)
	}
}
