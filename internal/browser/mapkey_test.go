package browser

import (
	"testing"

	"github.com/go-rod/rod/lib/input"
)

// mapKey 必须先去空格再判断；「 a 」这类输入曾因未 trim 落不进单字符分支，
// 静默退化成 Enter 触发错误按键。
func TestMapKeyTrimsAndExpands(t *testing.T) {
	cases := map[string]input.Key{
		"enter":     input.Enter,
		"  ENTER  ": input.Enter,
		" a ":       input.Key('a'),
		"A":         input.Key('A'),
		"left":      input.ArrowLeft,
		"del":       input.Delete,
		"pagedown":  input.PageDown,
		"esc":       input.Escape,
		"unknown":   input.Enter, // 兜底
	}
	for k, want := range cases {
		if got := mapKey(k); got != want {
			t.Errorf("mapKey(%q) = %v, want %v", k, got, want)
		}
	}
}
