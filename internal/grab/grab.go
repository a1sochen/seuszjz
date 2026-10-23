// Package grab 实现「蹲讲座 + 自动预约」的后台循环。
package grab

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"seuszjz/internal/seu"
)

// 抢座阶段。
const (
	PhaseIdle       = "idle"
	PhaseStarting   = "starting"
	PhaseWaiting    = "waiting"
	PhaseNeedCode   = "need_code"
	PhaseSubmitting = "submitting"
	PhaseLimited    = "limited"
	PhaseSuccess    = "success"
	PhaseFailed     = "failed"
	PhaseStopped    = "stopped"
)

var codePattern = regexp.MustCompile(`^[0-9A-Za-z]{4}$`)

// LogEntry 是一条抢座日志。
type LogEntry struct {
	At    string `json:"at"`
	Level string `json:"level"` // info / ok / warn / error
	Text  string `json:"text"`
}

// State 是抢座状态的快照, 会通过事件推送给前端。
type State struct {
	Running      bool       `json:"running"`
	WID          string     `json:"wid"`
	Name         string     `json:"name"`
	Phase        string     `json:"phase"`
	Message      string     `json:"message"`
	Attempts     int        `json:"attempts"`
	CaptchaCount int        `json:"captchaCount"`
	Captcha      string     `json:"captcha"` // data:image/jpeg;base64,... 需要手动输入时展示
	CaptchaAt    string     `json:"captchaAt"`
	StartedAt    string     `json:"startedAt"`
	UpdatedAt    string     `json:"updatedAt"`
	Interval     int        `json:"interval"`
	Cooldown     int        `json:"cooldown"`
	AutoCaptcha  bool       `json:"autoCaptcha"`
	Success      bool       `json:"success"`
	Logs         []LogEntry `json:"logs"`
}

// API 是抢座循环依赖的远端能力。
type API interface {
	Activities(ctx context.Context) ([]seu.Lecture, time.Time, error)
	FetchCaptcha(ctx context.Context) ([]byte, error)
	Reserve(ctx context.Context, wid, code string) (seu.Result, error)
}

// Options 是启动参数。
type Options struct {
	WID      string
	Interval time.Duration
	Cooldown time.Duration
	// Solve 为 nil 表示只能手动输入验证码。
	Solve   func(ctx context.Context, jpeg []byte) (string, error)
	OnState func(State)
	OnDone  func(State)
}

// Engine 是抢座循环。
type Engine struct {
	mu     sync.Mutex
	api    API
	state  State
	cancel context.CancelFunc
	done   chan struct{}
	manual chan string
	reload chan struct{}
	opts   Options
}

// New 创建一个引擎。
func New() *Engine {
	return &Engine{state: State{Phase: PhaseIdle, Logs: []LogEntry{}}}
}

// State 返回当前状态快照。
func (e *Engine) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cloneLocked()
}

func (e *Engine) cloneLocked() State {
	out := e.state
	logs := make([]LogEntry, len(e.state.Logs))
	copy(logs, e.state.Logs)
	out.Logs = logs
	return out
}

// Running 表示循环是否在跑。
func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state.Running
}

// Start 启动抢座循环; 已有循环在跑时返回错误。
func (e *Engine) Start(ctx context.Context, api API, opts Options) error {
	e.mu.Lock()
	if e.state.Running {
		e.mu.Unlock()
		return errors.New("抢座任务已在运行")
	}
	if opts.Interval <= 0 {
		opts.Interval = 3 * time.Second
	}
	if opts.Cooldown <= 0 {
		opts.Cooldown = 5 * time.Minute
	}
	e.api = api
	e.opts = opts
	runCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	e.done = make(chan struct{})
	e.manual = make(chan string, 4)
	e.reload = make(chan struct{}, 4)
	e.state = State{
		Running:     true,
		WID:         opts.WID,
		Phase:       PhaseStarting,
		Message:     "正在启动",
		StartedAt:   now(),
		UpdatedAt:   now(),
		Interval:    int(opts.Interval / time.Second),
		Cooldown:    int(opts.Cooldown / time.Second),
		AutoCaptcha: opts.Solve != nil,
		Logs:        []LogEntry{},
	}
	snapshot := e.cloneLocked()
	e.mu.Unlock()

	e.emit(snapshot)
	go e.run(runCtx)
	return nil
}

// Stop 停止循环并等待 goroutine 退出。
func (e *Engine) Stop() {
	e.mu.Lock()
	cancel, done := e.cancel, e.done
	// 清空句柄，避免重复 Stop 又等一次；run 里的 defer 已捕获 done，关闭不受影响。
	e.cancel, e.done = nil, nil
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

// SubmitCode 提交手动输入的验证码。
func (e *Engine) SubmitCode(code string) error {
	code = strings.TrimSpace(code)
	if !codePattern.MatchString(code) {
		return errors.New("请输入 4 位验证码")
	}
	select {
	case e.manual <- code:
		return nil
	default:
		return errors.New("当前不需要输入验证码")
	}
}

// RefreshCaptcha 让正在等待的循环换一张验证码。
func (e *Engine) RefreshCaptcha() {
	select {
	case e.reload <- struct{}{}:
	default:
	}
}

func (e *Engine) run(ctx context.Context) {
	defer close(e.done)
	defer func() {
		e.mu.Lock()
		e.state.Running = false
		if e.state.Phase != PhaseSuccess && e.state.Phase != PhaseFailed {
			e.state.Phase = PhaseStopped
		}
		if e.state.Phase == PhaseStopped {
			e.state.Message = "已停止"
		}
		e.state.Captcha = ""
		e.state.UpdatedAt = now()
		snapshot := e.cloneLocked()
		e.mu.Unlock()
		e.emit(snapshot)
		if e.opts.OnDone != nil {
			e.opts.OnDone(snapshot)
		}
	}()

	e.logf("info", "开始蹲守目标讲座")
	for {
		if ctx.Err() != nil {
			return
		}
		stop, err := e.round(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			e.fail(err.Error())
			return
		}
		if stop {
			return
		}
	}
}

// round 执行一轮检查, 返回是否结束。
func (e *Engine) round(ctx context.Context) (bool, error) {
	activities, _, err := e.api.Activities(ctx)
	if err != nil {
		return false, fmt.Errorf("读取讲座列表失败: %w", err)
	}
	wid := e.currentWID()
	var target *seu.Lecture
	for i := range activities {
		if activities[i].WID == wid {
			target = &activities[i]
			break
		}
	}
	if target == nil {
		return true, errors.New("目标讲座已从列表消失 (可能已下架)")
	}
	e.setName(target.Name)

	if target.Joined {
		e.succeed(target, "已经预约过该讲座")
		return true, nil
	}

	switch target.Status {
	case seu.StatusEnded, seu.StatusCancelled:
		return true, errors.New("讲座已结束或已取消")
	case seu.StatusFull, seu.StatusNotOpen:
		text := "名额已满, 等待退订"
		if target.Status == seu.StatusNotOpen {
			text = "尚未开放预约, 等待中"
		}
		e.update(func(s *State) {
			s.Phase = PhaseWaiting
			s.Message = text
			s.Captcha = ""
		})
		return false, e.sleep(ctx, e.opts.Interval)
	case seu.StatusAvailable:
		return e.attempt(ctx, target)
	default:
		// 其它状态 (已预约/已打卡等) 视作无需再抢
		e.succeed(target, "当前状态无需继续抢座: "+target.Status)
		return true, nil
	}
}

func (e *Engine) attempt(ctx context.Context, target *seu.Lecture) (bool, error) {
	e.update(func(s *State) {
		s.Phase = PhaseSubmitting
		s.Message = "检测到可预约, 正在提交"
		s.Attempts++
	})

	for try := 0; try < 6; try++ {
		if ctx.Err() != nil {
			return true, ctx.Err()
		}
		jpeg, err := e.api.FetchCaptcha(ctx)
		if err != nil {
			return false, fmt.Errorf("获取验证码失败: %w", err)
		}
		e.update(func(s *State) {
			s.CaptchaCount++
		})

		code := ""
		if e.opts.Solve != nil {
			solved, solveErr := e.opts.Solve(ctx, jpeg)
			if solveErr == nil && codePattern.MatchString(solved) {
				code = solved
				e.logf("info", "自动识别验证码: "+solved)
			} else if solveErr != nil {
				e.logf("warn", "自动识别失败: "+solveErr.Error())
			} else {
				e.logf("warn", "自动识别结果无效: "+solved)
			}
		}

		if code == "" {
			manual, err := e.waitManual(ctx, jpeg)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return true, err
				}
				if errors.Is(err, errReload) {
					continue
				}
				if errors.Is(err, errExpired) {
					e.logf("warn", "验证码等待超时, 重新获取")
					continue
				}
				return false, err
			}
			code = manual
		}

		result, err := e.api.Reserve(ctx, target.WID, code)
		if err != nil {
			return false, fmt.Errorf("提交预约失败: %w", err)
		}
		e.logf(levelFor(result), result.Message)

		switch result.Kind {
		case "ok":
			if result.Success {
				e.succeed(target, "预约成功")
				return true, nil
			}
		case "limited":
			e.update(func(s *State) {
				s.Phase = PhaseLimited
				s.Message = "请求过于频繁, 冷却中"
				s.Captcha = ""
			})
			e.logf("warn", fmt.Sprintf("触发限流, 冷却 %s", e.opts.Cooldown))
			return false, e.sleep(ctx, e.opts.Cooldown)
		case "vcode":
			e.update(func(s *State) { s.Captcha = "" })
			continue
		case "full":
			e.update(func(s *State) {
				s.Phase = PhaseWaiting
				s.Message = "名额已满, 继续等待"
				s.Captcha = ""
			})
			return false, e.sleep(ctx, e.opts.Interval)
		case "duplicated":
			e.succeed(target, result.Message)
			return true, nil
		case "closed":
			return true, fmt.Errorf("预约通道已关闭: %s", result.Message)
		default:
			if result.Success {
				e.succeed(target, result.Message)
				return true, nil
			}
			if result.Kind == "other" && strings.Contains(result.Message, "验证码") {
				continue
			}
			return false, e.sleep(ctx, e.opts.Interval)
		}
	}

	e.update(func(s *State) {
		s.Phase = PhaseWaiting
		s.Message = "多次提交未成功, 继续等待"
		s.Captcha = ""
	})
	return false, e.sleep(ctx, e.opts.Interval)
}

var (
	errExpired = errors.New("验证码过期")
	errReload  = errors.New("刷新验证码")
)

// waitManual 把验证码展示给用户并等待输入。
func (e *Engine) waitManual(ctx context.Context, jpeg []byte) (string, error) {
	dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpeg)
	e.update(func(s *State) {
		s.Phase = PhaseNeedCode
		s.Message = "请输入图片中的验证码"
		s.Captcha = dataURL
		s.CaptchaAt = now()
	})

	timeout := time.NewTimer(55 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-e.reload:
			return "", errReload
		case <-timeout.C:
			e.update(func(s *State) { s.Captcha = "" })
			return "", errExpired
		case code := <-e.manual:
			e.update(func(s *State) {
				s.Captcha = ""
				s.Phase = PhaseSubmitting
				s.Message = "正在提交验证码 " + code
			})
			return code, nil
		}
	}
}

func (e *Engine) succeed(target *seu.Lecture, message string) {
	e.update(func(s *State) {
		s.Phase = PhaseSuccess
		s.Success = true
		s.Message = message
		s.Captcha = ""
	})
	e.logf("ok", message+" | "+target.Name)
}

func (e *Engine) fail(message string) {
	e.update(func(s *State) {
		s.Phase = PhaseFailed
		s.Message = message
		s.Captcha = ""
	})
	e.logf("error", message)
}

func (e *Engine) setName(name string) {
	e.update(func(s *State) {
		if s.Name != name {
			s.Name = name
			s.Message = "目标: " + name
		}
	})
}

func (e *Engine) currentWID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state.WID
}

func (e *Engine) logf(level, text string) {
	e.mu.Lock()
	entry := LogEntry{At: now(), Level: level, Text: text}
	logs := append(e.state.Logs, entry)
	if len(logs) > 200 {
		logs = logs[len(logs)-200:]
	}
	e.state.Logs = logs
	e.state.UpdatedAt = now()
	snapshot := e.cloneLocked()
	e.mu.Unlock()
	e.emit(snapshot)
}

func (e *Engine) update(fn func(*State)) {
	e.mu.Lock()
	fn(&e.state)
	e.state.UpdatedAt = now()
	snapshot := e.cloneLocked()
	e.mu.Unlock()
	e.emit(snapshot)
}

func (e *Engine) emit(state State) {
	if e.opts.OnState != nil {
		e.opts.OnState(state)
	}
}

func (e *Engine) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		d = time.Second
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func now() string { return seu.Now().Format("15:04:05") }

func levelFor(result seu.Result) string {
	switch {
	case result.Success:
		return "ok"
	case result.Kind == "vcode":
		return "warn"
	case result.Kind == "limited":
		return "warn"
	default:
		return "error"
	}
}
