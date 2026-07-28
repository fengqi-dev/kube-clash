package mihomo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-clash/internal/cluster"
)

type RunningCore interface {
	io.Closer
	Done() <-chan struct{}
	Err() error
	Snapshot(context.Context) (Metrics, error)
}

type Metrics struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Connections   []Connection `json:"connections"`
	Memory        uint64       `json:"memory"`
}

type Connection struct {
	ID       string             `json:"id"`
	Metadata ConnectionMetadata `json:"metadata"`
	Upload   int64              `json:"upload"`
	Download int64              `json:"download"`
	Start    time.Time          `json:"start"`
	Chains   []string           `json:"chains"`
	Rule     string             `json:"rule"`
}

type ConnectionMetadata struct {
	Network         string `json:"network"`
	Type            string `json:"type"`
	SourceIP        string `json:"sourceIP"`
	DestinationIP   string `json:"destinationIP"`
	DestinationPort string `json:"destinationPort"`
	Host            string `json:"host"`
	Process         string `json:"process"`
	ProcessPath     string `json:"processPath"`
}

type Runtime struct {
	Installer    *Installer
	HTTPClient   *http.Client
	StartCommand func(string, string, io.Writer) (*exec.Cmd, error)
}

func (r *Runtime) Start(
	ctx context.Context,
	discovery cluster.Discovery,
	bridgeAddress string,
	_ string,
) (RunningCore, error) {
	host, rawPort, err := net.SplitHostPort(bridgeAddress)
	if err != nil {
		return nil, fmt.Errorf("parse SOCKS bridge address: %w", err)
	}
	bridgePort, err := strconv.Atoi(rawPort)
	if err != nil {
		return nil, fmt.Errorf("parse SOCKS bridge port: %w", err)
	}
	controllerPort, err := availablePort()
	if err != nil {
		return nil, err
	}
	secret, err := randomSecret()
	if err != nil {
		return nil, err
	}
	tunAddress, err := selectTUNAddress()
	if err != nil {
		return nil, err
	}
	config, err := Generate(discovery, Options{
		BridgeHost:       host,
		BridgePort:       bridgePort,
		ControllerPort:   controllerPort,
		ControllerSecret: secret,
		TUNAddress:       tunAddress,
	})
	if err != nil {
		return nil, err
	}
	installer := r.Installer
	if installer == nil {
		installer = &Installer{}
	}
	binaryPath, err := installer.Ensure(ctx)
	if err != nil {
		return nil, err
	}
	baseDir, err := installer.baseDir()
	if err != nil {
		return nil, err
	}
	sessionRoot := filepath.Join(baseDir, "sessions")
	if err := os.MkdirAll(sessionRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create mihomo session directory: %w", err)
	}
	workDir, err := os.MkdirTemp(sessionRoot, "session-*")
	if err != nil {
		return nil, fmt.Errorf("create mihomo working directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(workDir) }
	if err := os.WriteFile(filepath.Join(workDir, "config.yaml"), config, 0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("write mihomo config: %w", err)
	}
	logPath := filepath.Join(workDir, "mihomo.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create mihomo log: %w", err)
	}
	cmd, err := r.startCommand(binaryPath, workDir, logFile)
	if err != nil {
		logFile.Close()
		cleanup()
		return nil, err
	}
	process := &Process{
		cmd: cmd, done: make(chan struct{}), logFile: logFile,
		workDir: workDir, logPath: logPath,
		privilegedPIDPath: privilegedPIDPath(workDir, r.StartCommand == nil),
		controllerAddress: net.JoinHostPort("127.0.0.1", strconv.Itoa(controllerPort)),
		controllerSecret:  secret,
		httpClient:        r.HTTPClient,
	}
	go process.wait()
	go func() {
		select {
		case <-ctx.Done():
			_ = process.Close()
		case <-process.Done():
		}
	}()
	if err := r.waitReady(ctx, process); err != nil {
		logOutput := process.logTail()
		_ = process.Close()
		if logOutput != "" {
			return nil, fmt.Errorf("%w: %s", err, logOutput)
		}
		return nil, err
	}
	return process, nil
}

func (r *Runtime) startCommand(binaryPath, workDir string, output io.Writer) (*exec.Cmd, error) {
	if r.StartCommand != nil {
		return r.StartCommand(binaryPath, workDir, output)
	}
	return defaultStartCommand(binaryPath, workDir, output)
}

func (r *Runtime) waitReady(
	ctx context.Context,
	process *Process,
) error {
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		response, requestErr := process.request(ctx, "/version")
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready, tunErr := tunStartupStatus(process.logTail())
				if tunErr != nil {
					return tunErr
				}
				if ready {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-process.Done():
			if process.Err() != nil {
				return fmt.Errorf("mihomo exited before becoming ready: %w", process.Err())
			}
			return errors.New("mihomo exited before becoming ready")
		case <-deadline.C:
			return errors.New("timed out waiting for mihomo controller")
		case <-ticker.C:
		}
	}
}

type Process struct {
	cmd               *exec.Cmd
	done              chan struct{}
	logFile           *os.File
	logPath           string
	workDir           string
	privilegedPIDPath string
	controllerAddress string
	controllerSecret  string
	httpClient        *http.Client
	closeOnce         sync.Once
	errMu             sync.RWMutex
	waitErr           error
}

func (p *Process) Done() <-chan struct{} { return p.done }

func (p *Process) Err() error {
	p.errMu.RLock()
	defer p.errMu.RUnlock()
	return p.waitErr
}

func (p *Process) Snapshot(ctx context.Context) (Metrics, error) {
	response, err := p.request(ctx, "/connections")
	if err != nil {
		return Metrics{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Metrics{}, fmt.Errorf("mihomo connections API returned %s", response.Status)
	}
	var metrics Metrics
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(&metrics); err != nil {
		return Metrics{}, fmt.Errorf("decode mihomo connections: %w", err)
	}
	return metrics, nil
}

func (p *Process) request(ctx context.Context, path string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "http://"+p.controllerAddress+path, nil,
	)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+p.controllerSecret)
	client := p.httpClient
	if client == nil {
		client = &http.Client{Timeout: time.Second}
	}
	return client.Do(request)
}

func (p *Process) wait() {
	err := p.cmd.Wait()
	p.errMu.Lock()
	p.waitErr = err
	p.errMu.Unlock()
	_ = p.logFile.Close()
	close(p.done)
}

func (p *Process) Close() error {
	p.closeOnce.Do(func() {
		select {
		case <-p.done:
		default:
			if p.privilegedPIDPath != "" {
				_ = stopPrivilegedProcess(p.privilegedPIDPath)
			} else if p.cmd.Process != nil {
				if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
					_ = p.cmd.Process.Kill()
				}
			}
			select {
			case <-p.done:
			case <-time.After(5 * time.Second):
				if p.cmd.Process != nil {
					_ = p.cmd.Process.Kill()
				}
				<-p.done
			}
		}
		_ = os.RemoveAll(p.workDir)
	})
	err := p.Err()
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "signal") {
		return err
	}
	return nil
}

func (p *Process) logTail() string {
	content, err := os.ReadFile(p.logPath)
	if err != nil {
		return ""
	}
	const limit = 4096
	if len(content) > limit {
		content = content[len(content)-limit:]
	}
	return strings.TrimSpace(string(content))
}

func tunStartupStatus(logOutput string) (bool, error) {
	const failure = "Start TUN listening error:"
	if index := strings.LastIndex(logOutput, failure); index >= 0 {
		line := logOutput[index:]
		if newline := strings.IndexByte(line, '\n'); newline >= 0 {
			line = line[:newline]
		}
		return false, errors.New(strings.TrimSpace(line))
	}
	if strings.Contains(logOutput, "Tun adapter listening at:") ||
		(strings.Contains(logOutput, "Tun[") && strings.Contains(logOutput, "] proxy listening at:")) {
		return true, nil
	}
	return false, nil
}

func availablePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve mihomo controller port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func randomSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate mihomo controller secret: %w", err)
	}
	return hex.EncodeToString(value), nil
}
