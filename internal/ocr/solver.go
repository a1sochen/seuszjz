// Package ocr 把 github.com/only9464/ddddocr-go 的纯 Go 识别器包成 service 层
// 需要的那几个方法 (Describe / Available / Solve / Close)。
//
// 识别本身全在依赖库里: 权重 (20.4 MB 的主干 + 「空白 + 0-9」11 类分类头) 由
// ddddocr-go/digits 内嵌进二进制, 图片解码、Lanczos 缩放、conv/BiLSTM 前向和
// CTC 去重都由库实现。所以本包不再自带 weights.bin, 也不再需要
// scripts/tools/ocr_export.py 预先生成权重; 想换字符集只要把 digits 换成
// alnum(字母数字) 或 full(全字符集) 即可。
package ocr

import (
	"context"
	"errors"

	ddddocr "github.com/only9464/ddddocr-go"
	"github.com/only9464/ddddocr-go/digits"
)

// errNotReady 表示识别器还没加载好。
var errNotReady = errors.New("ocr: 识别器未初始化")

// Solver 是内置模型的验证码识别器, 满足 service 层需要的
// Describe / Available / Solve / Close 四个方法。
//
// 权重由 digits 包在进程内只加载一次, 识别器构造后只读、每次识别的中间缓冲都在
// 调用内新分配, 所以 Solver 可以被多个 goroutine 并发调用, 这里不需要加锁。
type Solver struct {
	rec *ddddocr.Recognizer
	err error
}

// New 构造识别器。权重在整个进程内只加载一次, 重复调用很便宜。
func New() *Solver {
	rec, err := digits.New()
	return &Solver{rec: rec, err: err}
}

// Describe 用于界面展示。
func (s *Solver) Describe() string {
	if s == nil {
		return ""
	}
	if s.err != nil {
		return "内置模型不可用: " + s.err.Error()
	}
	return "内置 Go 模型 · ddddocr-go · 无需 Python"
}

// Available 报告识别器是否可用。
func (s *Solver) Available(ctx context.Context) bool {
	if s == nil || s.err != nil || s.rec == nil {
		return false
	}
	return ctx.Err() == nil
}

// Close 目前没有需要释放的资源: 权重常驻内存, 不持有文件句柄。
func (s *Solver) Close() error { return nil }

// Solve 识别一张图片 (JPEG/PNG 原始字节), 返回 CTC 去重后的字符序列。
func (s *Solver) Solve(ctx context.Context, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	text, err := s.Recognise(data)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return text, nil
}

// Recognise 同步识别一张图片 (JPEG/PNG/GIF/BMP/TIFF/WebP 原始字节)。
func (s *Solver) Recognise(data []byte) (string, error) {
	if s == nil || s.rec == nil {
		return "", errNotReady
	}
	if s.err != nil {
		return "", s.err
	}
	return s.rec.Recognize(data)
}

// Charset 返回分类头的字符集, 下标 0 是 CTC 的 blank。
func (s *Solver) Charset() []string {
	if s == nil || s.rec == nil {
		return nil
	}
	return s.rec.Charset()
}

// Height 返回模型要求的输入图片高度 (common 模型是 64)。
func (s *Solver) Height() int {
	if s == nil || s.rec == nil {
		return 0
	}
	return s.rec.Height()
}
