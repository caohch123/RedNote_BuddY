package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"xhs-workbench/internal/tooling"
)

// XHSPublisherError is safe to expose: it never contains process output or
// upstream request/response text. A lost publish response must be reconciled
// manually, never retried automatically.
type XHSPublisherError struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	SubmissionUnknown bool   `json:"submission_unknown"`
	Unknown           bool   `json:"-"`
}

func (e *XHSPublisherError) Error() string { return e.Message }

type publisherReply struct {
	ID     uint64             `json:"id"`
	Result json.RawMessage    `json:"result"`
	Error  *XHSPublisherError `json:"error"`
}

type publisherProcess struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	responses chan publisherReply
	done      chan struct{}
	stop      func()
}

// XHSPublisher owns one isolated stdio worker. Calls are serialized because the
// upstream signing and Cookie lifecycle belong to one account.
type XHSPublisher struct {
	root     string
	gate     chan struct{}
	mu       sync.Mutex
	process  *publisherProcess
	closed   bool
	sequence uint64
	command  func() (*exec.Cmd, error) // injected only by transport tests
	ownTree  bool
	nodePath func() (string, error)
}

func NewXHSPublisher(root string, nodePath ...func() (string, error)) *XHSPublisher {
	p := &XHSPublisher{root: root, gate: make(chan struct{}, 1)}
	if len(nodePath) > 0 {
		p.nodePath = nodePath[0]
	}
	return p
}

func (p *XHSPublisher) resolveNode() (string, error) {
	configured := ""
	if p.nodePath != nil {
		var err error
		configured, err = p.nodePath()
		if err != nil {
			return "", &XHSPublisherError{Code: "dependency_missing", Message: "无法读取 Node.js 设置"}
		}
	}
	node, err := tooling.Resolve(p.root, configured, "node")
	if err != nil {
		return "", &XHSPublisherError{Code: "dependency_missing", Message: "请检查设置中的 Node.js 路径"}
	}
	return node, nil
}

func (p *XHSPublisher) workerCommand() (*exec.Cmd, error) {
	if p.command != nil {
		return p.command()
	}
	python := filepath.Join(p.root, "publisher", ".venv", "bin", "python")
	if runtime.GOOS == "windows" {
		python = filepath.Join(p.root, "publisher", ".venv", "Scripts", "python.exe")
	}
	if _, err := os.Stat(python); err != nil {
		return nil, &XHSPublisherError{Code: "dependency_missing", Message: "小红书发布组件待安装，请运行依赖安装脚本"}
	}
	node, err := p.resolveNode()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(python, "-u", filepath.Join(p.root, "publisher", "bridge.py"))
	cmd.Dir = filepath.Join(p.root, "publisher", "vendor", "Spider_XHS")
	// Do not inherit ambient Cookie files, debugging or secret-bearing output.
	for _, item := range os.Environ() {
		name := strings.ToUpper(strings.SplitN(item, "=", 2)[0])
		if name == "COOKIES" || strings.HasPrefix(name, "XHS_CREATOR_") || name == "PYTHONPATH" || name == "PYTHONUTF8" || name == "PYTHONIOENCODING" || name == "WORKBENCH_NODE_PATH" {
			continue
		}
		cmd.Env = append(cmd.Env, item)
	}
	path := filepath.Dir(node) + string(os.PathListSeparator) + os.Getenv("PATH")
	// Replace PATH instead of adding a second case-insensitive Windows entry.
	filtered := cmd.Env[:0]
	for _, item := range cmd.Env {
		if !strings.EqualFold(strings.SplitN(item, "=", 2)[0], "PATH") {
			filtered = append(filtered, item)
		}
	}
	cmd.Env = append(filtered, "PATH="+path, "WORKBENCH_NODE_PATH="+node, "PYTHONUTF8=1", "PYTHONIOENCODING=utf-8")
	hidePublisherWindow(cmd)
	return cmd, nil
}

func (p *XHSPublisher) start() (*publisherProcess, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, &XHSPublisherError{Code: "publisher_closed", Message: "小红书发布组件已关闭"}
	}
	if p.process != nil {
		return p.process, nil
	}
	cmd, err := p.workerCommand()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, publisherTransportError("dependency_missing", false)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, publisherTransportError("dependency_missing", false)
	}
	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, publisherTransportError("dependency_missing", false)
	}
	proc := &publisherProcess{cmd: cmd, stdin: stdin, responses: make(chan publisherReply, 1), done: make(chan struct{})}
	var once sync.Once
	stopTree := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	if p.ownTree {
		var treeErr error
		stopTree, treeErr = ownPublisherTree(cmd)
		if treeErr != nil {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, publisherTransportError("dependency_missing", false)
		}
	}
	proc.stop = func() { once.Do(func() { _ = stdin.Close(); stopTree() }) }
	p.process = proc
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 16<<20)
		for scanner.Scan() {
			var reply publisherReply
			if json.Unmarshal(scanner.Bytes(), &reply) != nil {
				break
			}
			select {
			case proc.responses <- reply:
			case <-proc.done:
				return
			}
		}
		// Malformed output or EOF terminates this worker. Captured bytes are
		// intentionally discarded rather than included in logs/errors.
		proc.stop()
	}()
	go func() { _ = cmd.Wait(); proc.stop(); close(proc.done) }()
	return proc, nil
}

func publisherTransportError(code string, unknown bool) *XHSPublisherError {
	message := "小红书发布组件连接中断，请重新连接账号"
	if code == "dependency_missing" {
		message = "小红书发布组件待安装，请运行依赖安装脚本"
	}
	if unknown {
		message = "发布请求结果不明，请先在小红书创作平台核实，禁止直接重复发布"
	}
	return &XHSPublisherError{Code: code, Message: message, SubmissionUnknown: unknown, Unknown: unknown}
}

func (p *XHSPublisher) invalidate(proc *publisherProcess) {
	p.mu.Lock()
	if p.process == proc {
		p.process = nil
	}
	p.mu.Unlock()
	proc.stop()
}

func (p *XHSPublisher) Call(ctx context.Context, method string, params any, result any) error {
	select {
	case p.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-p.gate }()
	if err := ctx.Err(); err != nil {
		return err
	}
	proc, err := p.start()
	if err != nil {
		return err
	}
	p.sequence++
	id := p.sequence
	request, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return errors.New("发布组件参数无法编码")
	}
	request = append(request, '\n')
	writeDone := make(chan error, 1)
	go func() { _, e := proc.stdin.Write(request); writeDone <- e }()
	select {
	case err = <-writeDone:
		if err != nil {
			p.invalidate(proc)
			return publisherTransportError("transport_error", method == "publish")
		}
	case <-ctx.Done():
		p.invalidate(proc)
		return publisherTransportError("request_cancelled", method == "publish")
	case <-proc.done:
		p.invalidate(proc)
		return publisherTransportError("transport_error", method == "publish")
	}
	select {
	case reply := <-proc.responses:
		if reply.ID != id {
			p.invalidate(proc)
			return publisherTransportError("protocol_error", method == "publish")
		}
		if reply.Error != nil {
			// Restrict messages to locally maintained codes. Even a corrupted
			// worker response may not reflect credential text to the frontend.
			message, ok := publisherMessages[reply.Error.Code]
			if !ok {
				message = "小红书发布组件操作失败"
				reply.Error.Code = "publisher_error"
			}
			reply.Error.Message = message
			reply.Error.Unknown = reply.Error.SubmissionUnknown
			return reply.Error
		}
		if result == nil {
			return nil
		}
		if err = json.Unmarshal(reply.Result, result); err != nil {
			return publisherTransportError("protocol_error", method == "publish")
		}
		return nil
	case <-ctx.Done():
		p.invalidate(proc)
		return publisherTransportError("request_cancelled", method == "publish")
	case <-proc.done:
		p.invalidate(proc)
		return publisherTransportError("transport_error", method == "publish")
	}
}

// publisherMessages covers exactly the codes publisher/bridge.py can emit.
// Unknown worker codes fall back to publisher_error above.
var publisherMessages = map[string]string{
	"dependency_missing":    "小红书发布组件依赖缺失，请运行依赖安装脚本",
	"invalid_request":       "发布组件参数无效",
	"unknown_method":        "发布组件不支持该操作",
	"login_not_found":       "二维码会话不存在，请重新生成二维码",
	"login_failed":          "二维码登录失败，请重新扫码或检查网络",
	"not_logged_in":         "请先扫码登录小红书账号",
	"session_invalid":       "登录信息已失效，请重新扫码登录",
	"account_check_failed":  "暂时无法核实账号，请检查网络后重试",
	"account_changed":       "登录账号与确认发布的账号不一致，请重新确认",
	"validation_failed":     "笔记标题、正文或媒体不符合发布要求",
	"asset_changed":         "发布素材已变化，请重新确认发布稿",
	"topic_not_found":       "未找到指定话题，请修改话题后重试",
	"upload_failed":         "素材上传失败，笔记尚未提交，可检查网络后重试",
	"publish_rejected":      "小红书拒绝了发布请求，请检查账号权限及笔记内容",
	"submission_unknown":    "发布结果不明，请先在小红书创作平台核实，禁止重复发布",
	"publisher_error":       "小红书发布组件操作失败",
}

func (p *XHSPublisher) Close() error {
	p.mu.Lock()
	p.closed = true
	proc := p.process
	p.process = nil
	p.mu.Unlock()
	if proc != nil {
		proc.stop()
	}
	return nil
}
