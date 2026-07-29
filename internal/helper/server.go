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
	"path/filepath"
	"sync"

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
	cmdDone <-chan struct{}
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
	reader := bufio.NewReader(conn)
	decoder := json.NewDecoder(reader)
	var request Request
	if err := decoder.Decode(&request); err != nil {
		_ = writeResponse(conn, Response{OK: false, Error: "invalid request"})
		return
	}
	response := s.dispatch(request)
	_ = writeResponse(conn, response)
}

func (s *Server) dispatch(request Request) Response {
	if request.Token == "" || request.Token != s.Auth.Token {
		return Response{OK: false, Error: "unauthorized"}
	}
	switch request.Op {
	case OpPing, OpStatus:
		return Response{
			OK: true, Version: Version, Installed: true, Running: true,
		}
	case OpStart:
		if err := ValidateWorkDir(request.WorkDir, s.Auth.HomeDir); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		if request.BinaryPath == "" {
			return Response{OK: false, Error: "binaryPath is required"}
		}
		if err := s.startSession(request.WorkDir, request.BinaryPath); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Version: Version}
	case OpStop:
		if err := ValidateWorkDir(request.WorkDir, s.Auth.HomeDir); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		if err := s.stopSession(request.WorkDir); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true, Version: Version}
	default:
		return Response{OK: false, Error: fmt.Sprintf("unsupported op %q", request.Op)}
	}
}

func (s *Server) startSession(workDir, binaryPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.sessions[workDir]; current != nil {
		return nil
	}
	logPath := filepath.Join(workDir, "sing-box.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open session log: %w", err)
	}
	cmd, err := singbox.StartLifecycleDirect(binaryPath, workDir, logFile)
	if err != nil {
		_ = logFile.Close()
		return err
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		close(done)
		s.mu.Lock()
		delete(s.sessions, workDir)
		s.mu.Unlock()
	}()
	s.sessions[workDir] = &session{workDir: workDir, cmdDone: done}
	return nil
}

func (s *Server) stopSession(workDir string) error {
	s.mu.Lock()
	current := s.sessions[workDir]
	s.mu.Unlock()
	if err := singbox.SignalLifecycleStop(workDir); err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	select {
	case <-current.cmdDone:
	default:
	}
	return nil
}

func writeResponse(w io.Writer, response Response) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(response)
}
