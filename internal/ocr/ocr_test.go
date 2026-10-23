package ocr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// refSample 是 refs.json 里的一条样例: 真实验证码图片 + 期望的识别结果。
// (逐时间步的前向对拍数据随迁移一起删了, 那部分覆盖在依赖库 ddddocr-go 自己的测试里。)
type refSample struct {
	Name string `json:"name"`
	File string `json:"file"`
	Text string `json:"text"`
}

type refFile struct {
	Charset []string    `json:"charset"`
	Height  int         `json:"height"`
	Samples []refSample `json:"samples"`
}

func loadRefs(t *testing.T) refFile {
	t.Helper()
	meta, err := os.ReadFile(filepath.Join("testdata", "refs.json"))
	if err != nil {
		t.Fatalf("读取 refs.json: %v", err)
	}
	var refs refFile
	if err := json.Unmarshal(meta, &refs); err != nil {
		t.Fatalf("解析 refs.json: %v", err)
	}
	if len(refs.Samples) == 0 {
		t.Fatal("refs.json 里没有样例")
	}
	return refs
}

func readSample(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("读取 %s: %v", name, err)
	}
	return data
}

// TestModel 校验识别器能加载, 且字符集/输入高度和 refs.json 记录的一致。
func TestModel(t *testing.T) {
	refs := loadRefs(t)
	solver := New()
	if !solver.Available(context.Background()) {
		t.Fatalf("模型不可用: %v", solver.err)
	}

	charset := solver.Charset()
	if len(charset) != len(refs.Charset) {
		t.Fatalf("字符集长度 %d, 期望 %d", len(charset), len(refs.Charset))
	}
	if charset[0] != "" {
		t.Fatalf("字符集下标 0 应为 CTC blank, 实际 %q", charset[0])
	}
	for i, ch := range charset {
		if ch != refs.Charset[i] {
			t.Fatalf("字符集第 %d 项 %q, 期望 %q", i, ch, refs.Charset[i])
		}
	}
	if got := solver.Height(); got != refs.Height {
		t.Fatalf("输入高度 %d, 期望 %d", got, refs.Height)
	}
	t.Logf("字符集 %d 类, 输入高度 %d, 说明 %q", len(charset), refs.Height, solver.Describe())
}

// TestSolveSamples 端到端跑真实验证码图片。
func TestSolveSamples(t *testing.T) {
	refs := loadRefs(t)
	solver := New()
	for _, sample := range refs.Samples {
		data := readSample(t, sample.File)
		text, err := solver.Solve(context.Background(), data)
		if err != nil {
			t.Fatalf("%s: %v", sample.File, err)
		}
		if text != sample.Text {
			t.Fatalf("%s: 识别 %q, 期望 %q", sample.File, text, sample.Text)
		}
		t.Logf("%s → %q", sample.File, text)
	}
}

// TestSolveBadImage 确认解码失败会以 error 返回, 而不是给出空串。
func TestSolveBadImage(t *testing.T) {
	if _, err := New().Solve(context.Background(), []byte("not an image")); err == nil {
		t.Fatal("非图片数据应当报错")
	}
}

// TestSolveCancelled 确认已经取消的 context 直接失败, 不去跑推理。
func TestSolveCancelled(t *testing.T) {
	refs := loadRefs(t)
	solver := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := solver.Solve(ctx, readSample(t, refs.Samples[0].File)); err == nil {
		t.Fatal("已取消的 context 应当报错")
	}
	if solver.Available(ctx) {
		t.Fatal("已取消的 context 下 Available 应当为 false")
	}
	if !solver.Available(context.Background()) {
		t.Fatalf("正常 context 下 Available 应为 true: %v", solver.err)
	}
}

// TestSolveConcurrent 同一个 Solver 被多个 goroutine 同时识别
// (库的识别器无锁共享, 配合 -race 跑)。
func TestSolveConcurrent(t *testing.T) {
	refs := loadRefs(t)
	solver := New()
	samples := make([][]byte, len(refs.Samples))
	for i, sample := range refs.Samples {
		samples[i] = readSample(t, sample.File)
	}

	const rounds = 4
	var wg sync.WaitGroup
	errs := make(chan error, len(samples)*rounds)
	for r := 0; r < rounds; r++ {
		for i, sample := range refs.Samples {
			wg.Add(1)
			go func(data []byte, want string) {
				defer wg.Done()
				text, err := solver.Solve(context.Background(), data)
				if err != nil {
					errs <- err
					return
				}
				if text != want {
					errs <- fmt.Errorf("识别结果 %q, 期望 %q", text, want)
				}
			}(samples[i], sample.Text)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestNilSolver 校验零值/空指针调用不会 panic。
func TestNilSolver(t *testing.T) {
	var solver *Solver
	if solver.Describe() != "" {
		t.Fatal("nil Solver 的 Describe 应返回空串")
	}
	if solver.Available(context.Background()) {
		t.Fatal("nil Solver 应当不可用")
	}
	if _, err := solver.Solve(context.Background(), nil); err == nil {
		t.Fatal("nil Solver 应当返回 error")
	}
	if err := solver.Close(); err != nil {
		t.Fatalf("nil Solver 的 Close 应返回 nil: %v", err)
	}

	zero := &Solver{}
	if _, err := zero.Recognise(nil); err == nil {
		t.Fatal("零值 Solver 应当返回 error, 而不是 panic")
	}
	if zero.Height() != 0 || zero.Charset() != nil {
		t.Fatal("零值 Solver 的 Height/Charset 应为零值")
	}
}
