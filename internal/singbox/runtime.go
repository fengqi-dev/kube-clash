package singbox

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

	"github.com/fengqi-dev/kube-loop/internal/cluster"
)

// PrivilegedStartFunc starts sing-box via an external privileged helper.
// It returns a stop function used during Close.
type PrivilegedStartFunc func(
	ctx context.Context, binaryPath, workDir string,
) (stop func(context.Context) error, err error)

type RunningCore interface {
	io.Closer
	Done() <-chan struct{}
	Err() error
	Snapshot(ctx context.Context) (Metrics, error)
}

const DefaultMetricsInterval = time.Second

type Runtime struct {
	Installer       *Installer
	HTTPClient      *http.Client
	StartCommand    func(string, string, io.Writer) (*exec.Cmd, error)
	PrivilegedStart PrivilegedStartFunc
}

func (r *Runtime) Start(
	ctx context.Context,
	discovery cluster.Discovery,
	bridgeAddress string,
	namespace string,
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
	dnsPort, err := selectDNSPort()
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
		DNSHost:          DefaultDNSListen,
		DNSPort:          dnsPort,
		TUNAddress:       tunAddress,
		Namespace:        namespace,
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
		return nil, fmt.Errorf("create sing-box session directory: %w", err)
	}
	workDir, err := os.MkdirTemp(sessionRoot, "session-*")
	if err != nil {
		return nil, fmt.Errorf("create sing-box working directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(workDir) }
	if err := os.WriteFile(filepath.Join(workDir, "config.json"), config, 0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("write sing-box config: %w", err)
	}
	dnsMeta, _ := json.Marshal(map[string]any{
		"listen":  DefaultDNSListen,
		"port":    dnsPort,
		"domains": ResolverDomains(namespace),
	})
	if err := os.WriteFile(filepath.Join(workDir, "dns-meta.json"), dnsMeta, 0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("write sing-box dns metadata: %w", err)
	}
	logPath := filepath.Join(workDir, "sing-box.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create sing-box log: %w", err)
	}
	process := &Process{
		done: make(chan struct{}), stopCh: make(chan struct{}), logFile: logFile,
		workDir: workDir, logPath: logPath,
		controllerAddress: net.JoinHostPort("127.0.0.1", strconv.Itoa(controllerPort)),
		controllerSecret:  secret,
		dnsPort:           dnsPort,
		resolverDomains:   ResolverDomains(namespace),
		httpClient:        r.HTTPClient,
	}
	if r.StartCommand == nil && r.PrivilegedStart != nil {
		stop, startErr := r.PrivilegedStart(ctx, binaryPath, workDir)
		if startErr != nil {
			logFile.Close()
			cleanup()
			return nil, startErr
		}
		process.useHelper = true
		process.helperStop = stop
		_ = logFile.Close()
		process.logFile = nil
	} else {
		cmd, startErr := r.startCommand(binaryPath, workDir, logFile)
		if startErr != nil {
			logFile.Close()
			cleanup()
			return nil, startErr
		}
		process.cmd = cmd
		process.privilegedPIDPath = privilegedPIDPath(
			workDir, r.StartCommand == nil && usesLifecycleWrapper(),
		)
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

func (r *Runtime) waitReady(ctx context.Context, process *Process) error {
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		response, requestErr := process.request(ctx, "/")
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusNotFound {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-process.Done():
			if err := process.Err(); err != nil {
				return err
			}
			return errors.New("sing-box exited before becoming ready")
		case <-deadline.C:
			return errors.New("timed out waiting for sing-box controller")
		case <-ticker.C:
		}
	}
}

type Process struct {
	cmd               *exec.Cmd
	done              chan struct{}
	stopCh            chan struct{}
	logFile           *os.File
	workDir           string
	logPath           string
	privilegedPIDPath string
	useHelper         bool
	helperStop        func(context.Context) error
	controllerAddress string
	controllerSecret  string
	dnsPort           int
	resolverDomains   []string
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
		return Metrics{}, fmt.Errorf("sing-box connections API returned %s", response.Status)
	}
	var raw clashConnections
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(&raw); err != nil {
		return Metrics{}, fmt.Errorf("decode sing-box connections: %w", err)
	}
	return mapClashMetrics(raw), nil
}

type clashConnections struct {
	DownloadTotal int64             `json:"downloadTotal"`
	UploadTotal   int64             `json:"uploadTotal"`
	Memory        uint64            `json:"memory"`
	Connections   []clashConnection `json:"connections"`
}

type clashConnection struct {
	ID       string `json:"id"`
	Metadata struct {
		Network         string `json:"network"`
		SourceIP        string `json:"sourceIP"`
		SourcePort      string `json:"sourcePort"`
		DestinationIP   string `json:"destinationIP"`
		DestinationPort string `json:"destinationPort"`
		Host            string `json:"host"`
		Process         string `json:"process"`
		ProcessPath     string `json:"processPath"`
	} `json:"metadata"`
	Upload   int64    `json:"upload"`
	Download int64    `json:"download"`
	Start    string   `json:"start"`
	Chains   []string `json:"chains"`
	Rule     string   `json:"rule"`
}

func mapClashMetrics(raw clashConnections) Metrics {
	connections := make([]Connection, 0, len(raw.Connections))
	for _, item := range raw.Connections {
		destination := item.Metadata.Host
		if destination == "" {
			destination = joinHostPort(item.Metadata.DestinationIP, item.Metadata.DestinationPort)
		} else if item.Metadata.DestinationPort != "" {
			destination = net.JoinHostPort(destination, item.Metadata.DestinationPort)
		}
		process := item.Metadata.Process
		if process == "" {
			process = filepath.Base(item.Metadata.ProcessPath)
		}
		outbound := DirectOutbound
		if len(item.Chains) > 0 {
			outbound = item.Chains[0]
		}
		connections = append(connections, Connection{
			ID:          item.ID,
			Network:     item.Metadata.Network,
			Source:      joinHostPort(item.Metadata.SourceIP, item.Metadata.SourcePort),
			Destination: destination,
			Process:     process,
			Upload:      item.Upload,
			Download:    item.Download,
			StartedAt:   item.Start,
			Outbound:    outbound,
			Rule:        item.Rule,
		})
	}
	if connections == nil {
		connections = []Connection{}
	}
	return Metrics{
		DownloadTotal:     raw.DownloadTotal,
		UploadTotal:       raw.UploadTotal,
		Memory:            raw.Memory,
		ActiveConnections: len(connections),
		Connections:       connections,
	}
}

func joinHostPort(host, port string) string {
	if host == "" && port == "" {
		return ""
	}
	if port == "" {
		return host
	}
	return net.JoinHostPort(host, port)
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
	if p.useHelper {
		<-p.stopCh
		p.errMu.Lock()
		p.waitErr = nil
		p.errMu.Unlock()
		close(p.done)
		return
	}
	err := p.cmd.Wait()
	p.errMu.Lock()
	p.waitErr = err
	p.errMu.Unlock()
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
	close(p.done)
}

func (p *Process) Close() error {
	p.closeOnce.Do(func() {
		select {
		case <-p.done:
		default:
			if p.useHelper {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if p.helperStop != nil {
					_ = p.helperStop(ctx)
				} else {
					_ = SignalLifecycleStop(p.workDir)
				}
				cancel()
				close(p.stopCh)
			} else if p.privilegedPIDPath != "" {
				_ = stopPrivilegedProcess(p.privilegedPIDPath)
			} else if p.cmd != nil && p.cmd.Process != nil {
				if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
					_ = p.cmd.Process.Kill()
				}
			}
			select {
			case <-p.done:
			case <-time.After(5 * time.Second):
				if p.cmd != nil && p.cmd.Process != nil {
					_ = p.cmd.Process.Kill()
				}
				if p.useHelper {
					select {
					case <-p.done:
					case <-time.After(2 * time.Second):
					}
				} else {
					<-p.done
				}
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
	text := string(content)
	if len(text) > 2000 {
		return text[len(text)-2000:]
	}
	return text
}

func availablePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func randomSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
