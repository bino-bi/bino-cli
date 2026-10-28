package daemon

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"bino.bi/bino/internal/logx"
)

// DefaultPreviewPort is the port a preview starts on when the caller names none.
const DefaultPreviewPort = 45678

// previewTailLines is how many output lines a PreviewProcess keeps for error reports.
const previewTailLines = 20

// previewMaxLine is the longest output line a PreviewProcess keeps.
const previewMaxLine = 64 * 1024

// PreviewInfo is the state of a preview subprocess.
type PreviewInfo struct {
	Status string `json:"status"` // "stopped", "starting", "running"
	URL    string `json:"url,omitempty"`
	Port   int    `json:"port,omitempty"`
}

// PreviewProcess runs at most one `bino preview` subprocess for a project. The
// daemon's HTTP routes and the MCP preview tools share one instance, so an
// editor and an agent see the same preview.
type PreviewProcess struct {
	root   string
	logger logx.Logger

	mu       sync.Mutex
	cmd      *exec.Cmd
	done     chan struct{} // closed when cmd has exited and been reaped
	port     int
	status   string
	url      string
	tail     []string
	onStatus func(PreviewInfo)
}

// NewPreviewProcess returns a stopped preview for the project at root.
func NewPreviewProcess(root string, logger logx.Logger) *PreviewProcess {
	if logger == nil {
		logger = logx.Nop()
	}
	return &PreviewProcess{root: root, logger: logger, status: "stopped"}
}

// SetOnStatus sets a callback for the running and stopped transitions.
func (p *PreviewProcess) SetOnStatus(fn func(PreviewInfo)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onStatus = fn
}

// Start launches the preview on port (DefaultPreviewPort when 0) with extra
// `bino preview` flags. When a preview already runs it returns its state and
// ignores port and args.
func (p *PreviewProcess) Start(port int, args ...string) (PreviewInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil {
		return p.infoLocked(), nil
	}

	exe, err := os.Executable()
	if err != nil {
		return PreviewInfo{}, fmt.Errorf("resolve executable: %w", err)
	}
	if port == 0 {
		port = DefaultPreviewPort
	}

	cmdArgs := append([]string{"preview", "--port", strconv.Itoa(port), "--work-dir", p.root}, args...)
	cmd := exec.CommandContext(context.Background(), exe, cmdArgs...) //nolint:gosec // G204: exe is our own binary
	cmd.Env = append(os.Environ(), "BINO_DISABLE_UPDATE_CHECK=1", "NO_COLOR=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return PreviewInfo{}, err
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout pipe

	if err := cmd.Start(); err != nil {
		return PreviewInfo{}, err
	}

	p.cmd = cmd
	p.done = make(chan struct{})
	p.port = port
	p.status = "starting"
	p.url = fmt.Sprintf("http://127.0.0.1:%d", port)
	p.tail = nil

	go p.watch(cmd, stdout, p.done)
	return p.infoLocked(), nil
}

// watch follows the subprocess output for the ready line and resets the state
// when the process exits.
func (p *PreviewProcess) watch(cmd *exec.Cmd, stdout io.Reader, done chan struct{}) {
	defer close(done)
	r := bufio.NewReaderSize(stdout, previewMaxLine)
	for {
		line, err := readLine(r)
		if err != nil {
			break
		}
		p.logger.Debugf("[preview] %s", line)
		p.mu.Lock()
		p.tail = append(p.tail, line)
		if len(p.tail) > previewTailLines {
			p.tail = p.tail[len(p.tail)-previewTailLines:]
		}
		ready := p.cmd == cmd && p.status == "starting" &&
			(strings.Contains(line, "Serving preview at") || strings.Contains(line, "http://"))
		if ready {
			p.status = "running"
		}
		info, notify := p.infoLocked(), p.onStatus
		p.mu.Unlock()
		if ready && notify != nil {
			notify(info)
		}
	}

	_ = cmd.Wait() //nolint:errcheck // reaping the exited preview process; its status is not consumed
	p.mu.Lock()
	switch {
	case p.cmd == cmd:
		p.resetLocked()
	case p.cmd != nil:
		// A new preview replaced this one; its state is not ours to report.
		p.mu.Unlock()
		return
	}
	// Stop already reset the state; clients still learn the process is gone.
	notify := p.onStatus
	p.mu.Unlock()
	if notify != nil {
		notify(PreviewInfo{Status: "stopped"})
	}
}

// readLine returns the next line without its newline, cut to previewMaxLine
// bytes. The rest of a longer line is read and dropped, so the child never
// blocks on a full pipe and later lines are still seen.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadSlice('\n')
	out := strings.TrimRight(string(line), "\r\n")
	for errors.Is(err, bufio.ErrBufferFull) {
		_, err = r.ReadSlice('\n')
	}
	if err != nil && (out == "" || !errors.Is(err, io.EOF)) {
		return out, err
	}
	return out, nil
}

// Info returns the current state.
func (p *PreviewProcess) Info() PreviewInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.infoLocked()
}

// Output returns the last lines the subprocess printed, for error reports.
func (p *PreviewProcess) Output() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.tail, "\n")
}

// Stop interrupts the subprocess and kills it if it is still alive after 3s.
// It does not wait; use Shutdown when the caller is about to exit.
func (p *PreviewProcess) Stop() {
	p.stop()
}

// Shutdown stops the subprocess and waits until it has exited, killing it
// once timeout passes. A process that exits right after Stop would otherwise
// leave the child running, because Stop's kill fallback dies with it.
func (p *PreviewProcess) Shutdown(timeout time.Duration) {
	cmd, done := p.stop()
	if cmd == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(timeout):
		_ = cmd.Process.Kill() //nolint:errcheck // last-resort kill; the process may already be gone
		<-done
	}
}

func (p *PreviewProcess) stop() (cmd *exec.Cmd, done chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cmd, done = p.cmd, p.done
	if cmd == nil || cmd.Process == nil {
		return nil, nil
	}

	// os.Interrupt is not supported on Windows; kill there instead.
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill() //nolint:errcheck // the process may already be gone
	}
	go func() {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill() //nolint:errcheck // last-resort kill; the process may already be gone
		}
	}()

	p.resetLocked()
	return cmd, done
}

func (p *PreviewProcess) infoLocked() PreviewInfo {
	return PreviewInfo{Status: p.status, URL: p.url, Port: p.port}
}

func (p *PreviewProcess) resetLocked() {
	p.cmd = nil
	p.done = nil
	p.status = "stopped"
	p.url = ""
	p.port = 0
}
