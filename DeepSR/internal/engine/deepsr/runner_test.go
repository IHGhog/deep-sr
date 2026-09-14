package deepsr

import (
	"strings"
	"testing"
)

func TestResolveInstalledModelNameNonExistent(t *testing.T) {
	// 测试不存在的带精度模型名，此前版本会因为包含 fp32 直接返回 nil，存在严重漏洞
	_, err := resolveInstalledModelName("non_existent_model_fp32", "")
	if err == nil {
		t.Fatalf("expected error for non existent model, got nil")
	}
	if !strings.Contains(err.Error(), "未检测到模型") {
		t.Errorf("unexpected error message: %v", err)
	}

	// 测试 CheckModelReady
	err = CheckModelReady("fake_anime_fp16", "", false)
	if err == nil {
		t.Fatalf("expected CheckModelReady to fail for fake_anime_fp16, got nil")
	}
}

func TestResolveInstalledModelNameReal(t *testing.T) {
	// 测试本地已下载存在的模型
	name, err := resolveInstalledModelName("realesr_animevideov3_fp32", "")
	if err != nil {
		t.Logf("Notice: realesr_animevideov3_fp32 not downloaded or error: %v", err)
	} else {
		if name != "realesr_animevideov3_fp32" {
			t.Errorf("expected name to be realesr_animevideov3_fp32, got %s", name)
		}
	}
}

func TestParseOutputLine(t *testing.T) {
	// 1. JSON 格式错误捕获
	{
		var lastErr string
		var lines []string
		jsonLine := `{"type":"error","message":"DirectML 显存分配失败"}`
		parseOutputLine(jsonLine, nil, &lastErr, &lines)
		if lastErr != "DirectML 显存分配失败" {
			t.Errorf("expected DirectML 显存分配失败, got: %s", lastErr)
		}
	}

	// 2. 非 JSON [ERROR] 格式错误提取
	{
		var lastErr string
		var lines []string
		errLine := `[ERROR] 找不到模型权重文件: models/custom.onnx`
		parseOutputLine(errLine, nil, &lastErr, &lines)
		if lastErr != "找不到模型权重文件: models/custom.onnx" {
			t.Errorf("expected extracted error, got: %s", lastErr)
		}
	}

	// 3. 非 JSON panic: 格式提取
	{
		var lastErr string
		var lines []string
		panicLine := `panic: runtime error: index out of range [0] with length 0`
		parseOutputLine(panicLine, nil, &lastErr, &lines)
		if !strings.HasPrefix(lastErr, "panic:") {
			t.Errorf("expected panic message, got: %s", lastErr)
		}
	}

	// 4. 普通滚动文本缓冲
	{
		var lastErr string
		var lines []string
		for i := 0; i < 15; i++ {
			parseOutputLine("line", nil, &lastErr, &lines)
		}
		if len(lines) != 10 {
			t.Errorf("expected 10 lines max in recent buffer, got: %d", len(lines))
		}
		if lastErr != "" {
			t.Errorf("expected empty lastErr for normal lines, got: %s", lastErr)
		}
	}
}
