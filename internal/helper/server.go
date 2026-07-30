package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

// Server is the privileged helper RPC server.
type Server struct {
	Auth AuthFile
	Log  *log.Logger

	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	workDir string
	cmd     *exec.Cmd
	done    chan struct{}
	routes  []string
	dns     singbox.DNSMeta
}

func NewServer(auth AuthFile) *Server {
	return &Server{
		Auth:     auth,
		Log:      log.Default(),
		sessions: map[string]*session{},
	}
}

func (s *Server) Serve(ctx context.Context) error {
	listener, err := listenHelper()
	if err != nil {
		return err
	}
	defer listener.Close()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	s.Log.Printf("kubeloop-helper listening on %s (version %s)", SocketPath(), Version)
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				s.stopAllSessions()
				return nil
			default:
				return err
			}
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	decoder := json.NewDecoder(bufio.NewReader(conn))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		_ = writeResponse(conn, Response{OK: false, Error: "invalid request"})
		return
	}
	response := s.dispatch(request)
	if err := writeResponse(conn, response); err != nil &&
		request.Op == OpStart && response.OK && request.Session != nil {
		_ = s.stopSession(request.Session.ID)
	}
}

func (s *Server) dispatch(request Request) Response {
	if request.Token == "" || request.Token != s.Auth.Token {
		return Response{OK: false, Error: "unauthorized"}
	}
	switch request.Op {
	case OpPing, OpStatus:
		return Response{
			OK: true, Version: Version, Protocol: ProtocolVersion,
			Installed: true, Running: true,
		}
	case OpStart:
		if request.Session == nil {
			return Response{OK: false, Error: "session is required"}
		}
		if err := s.startSession(*request.Session); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Version: Version, Protocol: ProtocolVersion}
	case OpStop:
		if err := singbox.ValidateSessionID(request.SessionID); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		if err := s.stopSession(request.SessionID); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Version: Version, Protocol: ProtocolVersion}
	default:
		return Response{OK: false, Error: fmt.Sprintf("unsupported op %q", request.Op)}
	}
}

func (s *Server) startSession(spec singbox.SessionSpec) error {
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("validate session: %w", err)
	}
	config, err := spec.GenerateConfig()
	if err != nil {
		return fmt.Errorf("generate sing-box config: %w", err)
	}
	dns, err := spec.DNS()
	if err != nil {
		return fmt.Errorf("build DNS settings: %w", err)
	}
	routes, err := spec.Routes()
	if err != nil {
		return fmt.Errorf("build route settings: %w", err)
	}

	current := &session{done: make(chan struct{}), routes: routes, dns: dns}
	s.mu.Lock()
	if existing := s.sessions[spec.ID]; existing != nil {
		s.mu.Unlock()
		return nil
	}
	if len(s.sessions) != 0 {
		s.mu.Unlock()
		return fmt.Errorf("another privileged TUN session is already active")
	}
	s.sessions[spec.ID] = current
	s.mu.Unlock()
	fail := func() {
		s.mu.Lock()
		delete(s.sessions, spec.ID)
		s.mu.Unlock()
	}

	sessionRoot := filepath.Join(SystemStateDir(), "sessions")
	if err := os.MkdirAll(sessionRoot, 0o700); err != nil {
		fail()
		return fmt.Errorf("create protected session root: %w", err)
	}
	current.workDir = filepath.Join(sessionRoot, spec.ID)
	if err := os.RemoveAll(current.workDir); err != nil {
		fail()
		return fmt.Errorf("clear stale protected session: %w", err)
	}
	if err := os.Mkdir(current.workDir, 0o700); err != nil {
		fail()
		return fmt.Errorf("create protected session: %w", err)
	}
	cleanupFiles := func() { _ = os.RemoveAll(current.workDir) }
	if err := os.WriteFile(filepath.Join(current.workDir, "config.json"), config, 0o600); err != nil {
		cleanupFiles()
		fail()
		return fmt.Errorf("write protected sing-box config: %w", err)
	}
	meta, _ := json.Marshal(dns)
	if err := os.WriteFile(filepath.Join(current.workDir, "dns-meta.json"), meta, 0o600); err != nil {
		cleanupFiles()
		fail()
		return fmt.Errorf("write protected DNS metadata: %w", err)
	}

	installer := &singbox.Installer{
		BaseDir:         SystemStateDir(),
		DisableOverride: true,
	}
	binaryPath, err := installer.Ensure(context.Background())
	if err != nil {
		cleanupFiles()
		fail()
		return fmt.Errorf("ensure trusted sing-box core: %w", err)
	}
	logFile, err := os.OpenFile(
		filepath.Join(current.workDir, "sing-box.log"),
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600,
	)
	if err != nil {
		cleanupFiles()
		fail()
		return fmt.Errorf("open protected session log: %w", err)
	}
	if err := applyPlatformDNS(current.workDir, dns); err != nil {
		_ = restorePlatformDNS(current.workDir, dns)
		_ = logFile.Close()
		cleanupFiles()
		fail()
		return fmt.Errorf("install split DNS: %w", err)
	}

	cmd := exec.Command(
		binaryPath, "run", "-c", filepath.Join(current.workDir, "config.json"),
		"-D", current.workDir,
	)
	cmd.Dir = current.workDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = restorePlatformDNS(current.workDir, dns)
		_ = logFile.Close()
		cleanupFiles()
		fail()
		return fmt.Errorf("start trusted sing-box core: %w", err)
	}
	s.mu.Lock()
	current.cmd = cmd
	s.mu.Unlock()
	go func() {
		waitErr := cmd.Wait()
		if waitErr != nil {
			s.Log.Printf("sing-box session %s exited: %v", spec.ID, waitErr)
		}
		_ = restorePlatformDNS(current.workDir, current.dns)
		cleanupPlatformRoutes(current.routes)
		_ = logFile.Close()
		_ = os.RemoveAll(current.workDir)
		s.mu.Lock()
		delete(s.sessions, spec.ID)
		s.mu.Unlock()
		close(current.done)
	}()
	return nil
}

func (s *Server) stopSession(sessionID string) error {
	s.mu.Lock()
	current := s.sessions[sessionID]
	if current == nil {
		s.mu.Unlock()
		return nil
	}
	cmd := current.cmd
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("session is still starting")
	}
	if err := stopManagedProcess(cmd.Process); err != nil {
		s.Log.Printf("graceful stop for session %s failed: %v", sessionID, err)
		_ = cmd.Process.Kill()
	}
	select {
	case <-current.done:
		return nil
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		select {
		case <-current.done:
			return nil
		case <-time.After(5 * time.Second):
			return fmt.Errorf("timed out waiting for privileged session to stop")
		}
	}
}

func (s *Server) stopAllSessions() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		_ = s.stopSession(id)
	}
}

func writeResponse(w io.Writer, response Response) error {
	return json.NewEncoder(w).Encode(response)
}
