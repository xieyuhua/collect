package jsonx

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type token struct {
	kind string // key|index|wild
	key  string
	idx  int
}

func tokenize(path string) []token {
	var out []token
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, token{kind: "key", key: buf.String()})
			buf.Reset()
		}
	}
	rs := []rune(path)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch c {
		case '.':
			flush()
		case '[':
			flush()
			j := i + 1
			var sb strings.Builder
			quote := rune(0)
			for ; j < len(rs); j++ {
				ch := rs[j]
				if quote != 0 {
					if ch == quote {
						quote = 0
						continue
					}
					sb.WriteRune(ch)
					continue
				}
				if ch == '"' || ch == '\'' {
					quote = ch
					continue
				}
				if ch == ']' {
					break
				}
				sb.WriteRune(ch)
			}
			inner := strings.TrimSpace(sb.String())
			i = j
			if inner == "*" {
				out = append(out, token{kind: "wild"})
			} else if n, err := strconv.Atoi(inner); err == nil {
				out = append(out, token{kind: "index", idx: n})
			} else {
				out = append(out, token{kind: "key", key: inner})
			}
		default:
			buf.WriteRune(c)
		}
	}
	flush()
	return out
}

func Lookup(root any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return root, true
	}
	return lookup(root, tokenize(path))
}

func lookup(v any, ts []token) (any, bool) {
	if len(ts) == 0 {
		return v, true
	}
	t := ts[0]
	switch t.kind {
	case "key":
		switch m := v.(type) {
		case map[string]any:
			nv, ok := m[t.key]
			if !ok {
				return nil, false
			}
			return lookup(nv, ts[1:])
		case map[string]string:
			nv, ok := m[t.key]
			if !ok {
				return nil, false
			}
			return lookup(nv, ts[1:])
		}
		return nil, false
	case "index":
		if arr, ok := v.([]any); ok {
			if t.idx >= 0 && t.idx < len(arr) {
				return lookup(arr[t.idx], ts[1:])
			}
		}
		return nil, false
	case "wild":
		arr, ok := v.([]any)
		if !ok {
			return nil, false
		}
		out := make([]any, 0, len(arr))
		for _, item := range arr {
			if r, ok := lookup(item, ts[1:]); ok {
				out = append(out, r)
			}
		}
		return out, true
	}
	return nil, false
}

func Stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case json.Number:
		return x.String()
	case map[string]any, []any:
		b, _ := json.Marshal(x)
		return string(b)
	default:
		return fmt.Sprint(x)
	}
}

func LookupString(root any, path string) string {
	v, ok := Lookup(root, path)
	if !ok {
		return ""
	}
	return Stringify(v)
}

// Flatten 将对象展开为 a.b.c 形式的键值（数组使用下标）
func Flatten(v any) map[string]any {
	out := map[string]any{}
	flatten(v, "", out, 0)
	return out
}

func flatten(v any, prefix string, out map[string]any, depth int) {
	if depth > 8 {
		return
	}
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 && prefix != "" {
			out[prefix] = x
			return
		}
		for k, val := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			flatten(val, p, out, depth+1)
		}
	case []any:
		if len(x) == 0 && prefix != "" {
			out[prefix] = x
			return
		}
		for i, val := range x {
			flatten(val, fmt.Sprintf("%s[%d]", prefix, i), out, depth+1)
		}
	default:
		if prefix != "" {
			out[prefix] = v
		}
	}
}

type ArrayHit struct {
	Path  string
	Count int
}

// Arrays 找出所有对象数组
func Arrays(v any) []ArrayHit {
	var out []ArrayHit
	walkArrays(v, "", &out, 0)
	return out
}

func walkArrays(v any, prefix string, out *[]ArrayHit, depth int) {
	if depth > 10 {
		return
	}
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			walkArrays(val, p, out, depth+1)
		}
	case []any:
		if len(x) > 0 {
			if _, ok := x[0].(map[string]any); ok {
				*out = append(*out, ArrayHit{Path: prefix, Count: len(x)})
			}
		}
		for i, val := range x {
			walkArrays(val, fmt.Sprintf("%s[%d]", prefix, i), out, depth+1)
		}
	}
}

// SetPath 在 JSON 对象中按路径设置值（仅支持对象键路径）
func SetPath(root map[string]any, path string, value any) bool {
	parts := strings.Split(path, ".")
	cur := root
	for i, p := range parts {
		if i == len(parts)-1 {
			cur[p] = value
			return true
		}
		nxt, ok := cur[p].(map[string]any)
		if !ok {
			nxt = map[string]any{}
			cur[p] = nxt
		}
		cur = nxt
	}
	return false
}
