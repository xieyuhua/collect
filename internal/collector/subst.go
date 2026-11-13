package collector

import (
	"encoding/base64"
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var varRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.\-]+)\s*(?:\|\s*([a-zA-Z]+)\s*)?\}\}`)

// Vars 运行期变量表，支持 {{name|filter}} 渲染
type Vars struct {
	mu sync.RWMutex
	m  map[string]string
}

func NewVars() *Vars { return &Vars{m: map[string]string{}} }

func (v *Vars) Set(k, val string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.m[k] = val
}

func (v *Vars) Get(k string) string {
	v.mu.RLock()
	val, ok := v.m[k]
	v.mu.RUnlock()
	if ok {
		return val
	}
	switch k {
	case "now":
		return time.Now().Format("2006-01-02 15:04:05")
	case "today":
		return time.Now().Format("2006-01-02")
	case "timestamp":
		return strconv.FormatInt(time.Now().UnixMilli(), 10)
	case "ts":
		return strconv.FormatInt(time.Now().Unix(), 10)
	case "uuid":
		return uuid.NewString()
	case "random":
		return strconv.Itoa(rand.Intn(1000000))
	}
	return ""
}

func (v *Vars) Has(k string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	_, ok := v.m[k]
	return ok
}

func (v *Vars) All() map[string]string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make(map[string]string, len(v.m))
	for k, val := range v.m {
		out[k] = val
	}
	return out
}

// Render 替换文本中的 {{变量}}
func (v *Vars) Render(s string) string {
	if s == "" || !strings.Contains(s, "{{") {
		return s
	}
	return varRe.ReplaceAllStringFunc(s, func(match string) string {
		sub := varRe.FindStringSubmatch(match)
		name := sub[1]
		filter := sub[2]
		val, ok := v.lookup(name)
		if !ok {
			return match
		}
		switch strings.ToLower(filter) {
		case "url", "urlencode":
			return url.QueryEscape(val)
		case "b64", "base64":
			return base64.StdEncoding.EncodeToString([]byte(val))
		case "lower":
			return strings.ToLower(val)
		case "upper":
			return strings.ToUpper(val)
		case "raw":
			return val
		}
		return val
	})
}

func (v *Vars) lookup(name string) (string, bool) {
	v.mu.RLock()
	val, ok := v.m[name]
	v.mu.RUnlock()
	if ok {
		return val, true
	}
	switch name {
	case "now":
		return time.Now().Format("2006-01-02 15:04:05"), true
	case "today":
		return time.Now().Format("2006-01-02"), true
	case "timestamp":
		return strconv.FormatInt(time.Now().UnixMilli(), 10), true
	case "ts":
		return strconv.FormatInt(time.Now().Unix(), 10), true
	case "uuid":
		return uuid.NewString(), true
	case "random":
		return strconv.Itoa(rand.Intn(1000000)), true
	}
	return "", false
}

// RenderBody JSON 请求体渲染：数字类变量自动去引号
func (v *Vars) RenderBody(s string) string {
	if s == "" {
		return s
	}
	for _, k := range []string{"page", "pageSize", "offset", "limit", "start", "end", "size"} {
		if val, ok := v.lookup(k); ok && isNumeric(val) {
			s = strings.ReplaceAll(s, `"`+"{{"+k+"}}"+`"`, "{{"+k+"}}")
		}
	}
	return v.Render(s)
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func mustJSON(v any) string {
	b, err := jsonMarshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
