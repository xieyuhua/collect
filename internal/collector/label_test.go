package collector

import (
	"testing"

	"collect/internal/model"
)

// 步骤日志的 desc 是中文，按字节截断会把汉字切成乱码；必须按 rune 截
func TestUIStepLabelTruncatesByRune(t *testing.T) {
	s := model.UIStep{Desc: "点击 定点药店药品量价比较指数 这一菜单项查看详情并跳转到对应的报表页面进行下一步分析操作流程"}
	label := uiStepLabel(s)
	nr := len([]rune(label))
	if nr > 40 {
		t.Fatalf("label too long: %d runes", nr)
	}
	if string([]rune(label)) != label {
		t.Fatalf("label broke UTF-8: %q", label)
	}
	if nr != 40 {
		t.Fatalf("expected 40 runes, got %d: %q", nr, label)
	}
}

func TestUIStepLabelFallsBack(t *testing.T) {
	label := uiStepLabel(model.UIStep{Action: "click", Selector: "#btn", Value: "x"})
	if label == "" {
		t.Fatal("empty label")
	}
}
