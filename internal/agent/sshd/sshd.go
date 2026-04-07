package sshd

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
	gossh "github.com/gliderlabs/ssh"
)

type Server struct {
	logger *slog.Logger
	port   int
	srv    *gossh.Server
}

func New(logger *slog.Logger, port int) *Server {
	return &Server{
		logger: logger,
		port:   port,
	}
}

func (s *Server) authHandler(_ gossh.Context, _ gossh.PublicKey) bool {
	// Accept all connections until token-based auth is implemented.
	return true
}

func (s *Server) Close() error {
	if s.srv != nil {
		return s.srv.Close()
	}
	return nil
}

func (s *Server) Serve() error {
	srv := &gossh.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler:           s.sessionHandler,
		PublicKeyHandler:  s.authHandler,
		SubsystemHandlers: map[string]gossh.SubsystemHandler{},
		LocalPortForwardingCallback: func(ctx gossh.Context, destinationHost string, destinationPort uint32) bool {
			return true
		},
		ReversePortForwardingCallback: func(ctx gossh.Context, bindHost string, bindPort uint32) bool {
			return true
		},
		ChannelHandlers: map[string]gossh.ChannelHandler{
			"session":      gossh.DefaultSessionHandler,
			"direct-tcpip": gossh.DirectTCPIPHandler,
		},
		RequestHandlers: map[string]gossh.RequestHandler{
			"tcpip-forward":        (&gossh.ForwardedTCPHandler{}).HandleSSHRequest,
			"cancel-tcpip-forward": (&gossh.ForwardedTCPHandler{}).HandleSSHRequest,
		},
	}

	s.srv = srv
	s.logger.Info("ssh server starting", "port", s.port)
	return srv.ListenAndServe()
}

func (s *Server) sessionHandler(sess gossh.Session) {
	ptyReq, winCh, isPTY := sess.Pty()
	s.logger.Info("ssh session", "pty", isPTY, "command", sess.RawCommand(), "user", sess.User())

	if !isPTY {
		rawCmd := sess.RawCommand()

		var cmd *exec.Cmd
		if rawCmd == "" {
			// No PTY, no command: start a shell on stdin/stdout.
			// VS Code Remote SSH uses this to install its server.
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "/bin/sh"
			}
			cmd = exec.Command(shell)
		} else {
			cmd = exec.Command("/bin/sh", "-c", rawCmd)
		}

		home, _ := os.UserHomeDir()
		if home != "" {
			cmd.Dir = home
		}

		cmd.Env = append(os.Environ(), sess.Environ()...)
		cmd.Stdout = sess
		cmd.Stderr = sess.Stderr()

		stdin, err := cmd.StdinPipe()
		if err != nil {
			s.logger.Error("stdin pipe failed", "error", err)
			sess.Exit(1)
			return
		}
		go func() {
			io.Copy(stdin, sess)
			stdin.Close()
		}()

		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				sess.Exit(exitErr.ExitCode())
				return
			}
			s.logger.Error("command failed", "error", err)
			sess.Exit(1)
			return
		}
		sess.Exit(0)
		return
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell, "-l")
	if sess.RawCommand() != "" {
		cmd = exec.Command("/bin/sh", "-c", sess.RawCommand())
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		cmd.Dir = home
	}

	cmd.Env = append(os.Environ(), sess.Environ()...)
	cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
	cmd.Env = append(cmd.Env, fmt.Sprintf("HOME=%s", home))

	ptmx, err := pty.Start(cmd)
	if err != nil {
		s.logger.Error("pty start failed", "error", err, "shell", shell)
		sess.Exit(1)
		return
	}
	defer ptmx.Close()

	s.logger.Info("pty started", "shell", cmd.Path, "pid", cmd.Process.Pid)

	setWinsize(ptmx, ptyReq.Window.Width, ptyReq.Window.Height)

	go func() {
		for win := range winCh {
			setWinsize(ptmx, win.Width, win.Height)
		}
	}()

	go func() {
		io.Copy(ptmx, sess)
		s.logger.Info("stdin->pty copy ended")
	}()

	n, copyErr := io.Copy(sess, ptmx)
	s.logger.Info("pty->stdout copy ended", "bytes", n, "error", copyErr)

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			sess.Exit(exitErr.ExitCode())
			return
		}
	}
	sess.Exit(0)
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(
		syscall.SYS_IOCTL,
		f.Fd(),
		uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct {
			h, w, x, y uint16
		}{
			uint16(h), uint16(w), 0, 0,
		})),
	)
}
