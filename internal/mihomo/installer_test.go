package mihomo

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestExtractGzipExecutable(t *testing.T) {
	var archive bytes.Buffer
	writer := gzip.NewWriter(&archive)
	if _, err := writer.Write([]byte("mihomo-binary")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	value, err := extractExecutable("mihomo.gz", archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "mihomo-binary" {
		t.Fatalf("unexpected executable: %q", value)
	}
}

func TestExtractZipExecutable(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	file, err := writer.Create("mihomo.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("mihomo-windows")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	value, err := extractExecutable("mihomo.zip", archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "mihomo-windows" {
		t.Fatalf("unexpected executable: %q", value)
	}
}

func TestVerifySHA256(t *testing.T) {
	content := []byte("verified")
	sum := sha256.Sum256(content)
	if err := verifySHA256(content, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if err := verifySHA256(content, "invalid"); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestReleaseAssetsHaveChecksums(t *testing.T) {
	for platform, asset := range releaseAssets {
		if len(asset.SHA256) != sha256.Size*2 {
			t.Errorf("%s has invalid SHA-256 %q", platform, asset.SHA256)
		}
		if asset.Name == "" {
			t.Errorf("%s has no release filename", platform)
		}
	}
}

func TestInstallerDownloadsVerifiesAndInstalls(t *testing.T) {
	var archive bytes.Buffer
	writer := gzip.NewWriter(&archive)
	if _, err := writer.Write([]byte("mihomo-binary")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())
	downloads := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(request.URL.String(), "https://") {
			t.Fatalf("installer test expected HTTPS, got %s", request.URL)
		}
		downloads++
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(archive.Bytes())),
			Request:    request,
		}, nil
	})}
	asset := releaseAsset{Name: "mihomo-test.gz", SHA256: hex.EncodeToString(sum[:])}
	installer := &Installer{
		HTTPClient: client,
		BaseDir:    t.TempDir(),
		GOOS:       "darwin",
		GOARCH:     "arm64",
		Asset:      &asset,
		DownloadURL: func(releaseAsset) string {
			return "https://example.invalid/mihomo.gz"
		},
	}
	path, err := installer.Ensure(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "mihomo-binary" {
		t.Fatalf("unexpected installed content: %q", content)
	}
	if downloads != 1 {
		t.Fatalf("expected one download, got %d", downloads)
	}
	if _, err := installer.Ensure(t.Context()); err != nil {
		t.Fatal(err)
	}
	if downloads != 1 {
		t.Fatalf("existing binary was downloaded again: %d", downloads)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
