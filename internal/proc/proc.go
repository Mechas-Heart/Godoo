// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package proc is a minimal process supervisor: start a command detached
// from the terminal with its output in a log file, remember it in a pid
// file, and stop it later with a signal.
//
// Debugging tip: every service is just `cat .godoo/run/<svc>.pid` and
// `tail -f .godoo/logs/<svc>.log` - no daemon in between.
package proc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Spec struct {
	Name       string
	Args       []string // Args[0] is the executable
	Env        []string
	Dir        string
	LogFile    string
	PidFile    string
	StopSignal syscall.Signal // defaults to SIGTERM
}

// Handle is a process started by this invocation of godoo.
type Handle struct {
	Pid    int
	exited chan struct{}
	err    error
}

// Exited reports whether the process already terminated (and why).
func (h *Handle) Exited() (bool, error) {
	select {
	case <-h.exited:
		return true, h.err
	default:
		return false, nil
	}
}

// Start launches the process in its own session so it survives godoo exiting.
func Start(s Spec) (*Handle, error) {
	log, err := os.OpenFile(s.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer log.Close()
	fmt.Fprintf(log, "\n----- %s: %s\n", time.Now().Format(time.RFC3339), strings.Join(s.Args, " "))

	cmd := exec.Command(s.Args[0], s.Args[1:]...)
	cmd.Env, cmd.Dir = s.Env, s.Dir
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", s.Name, err)
	}
	if err := os.WriteFile(s.PidFile, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	h := &Handle{Pid: cmd.Process.Pid, exited: make(chan struct{})}
	// Reap the child while godoo is still running so an early crash is
	// noticed instead of lingering as a zombie that looks alive.
	go func() { h.err = cmd.Wait(); close(h.exited) }()
	return h, nil
}

// Running returns the pid from pidFile if that process is alive.
func Running(pidFile string) (int, bool) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	// Signal 0 checks existence without affecting the process.
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return 0, false
	}
	return pid, true
}

// Stop signals the process and waits for it to exit, escalating to SIGKILL
// after timeout. A missing or stale pid file is not an error.
func Stop(pidFile string, sig syscall.Signal, timeout time.Duration) error {
	pid, ok := Running(pidFile)
	if !ok {
		_ = os.Remove(pidFile)
		return nil
	}
	if sig == 0 {
		sig = syscall.SIGTERM
	}
	if err := syscall.Kill(pid, sig); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, alive := Running(pidFile); !alive {
			return os.Remove(pidFile)
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return os.Remove(pidFile)
}

// WaitPort waits until something accepts TCP connections on 127.0.0.1:port.
// It gives up early if h (optional) reports that the process exited.
func WaitPort(port int, h *Handle, timeout time.Duration) error {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			conn.Close()
			return nil
		}
		if h != nil {
			if exited, err := h.Exited(); exited {
				return fmt.Errorf("process exited before listening on %s (%v)", addr, err)
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("nothing listening on %s after %s", addr, timeout)
}

// Tail returns the last n lines of a file, for error messages.
func Tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
