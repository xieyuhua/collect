package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	ID    int64  `json:"id"`
	Time  string `json:"time"`
	Level string `json:"level"`
	Scope string `json:"scope"`
	Msg   string `json:"msg"`
}

// Logger 同时写文件与内存环形缓冲，供平台"运行日志"页面查看
type Logger struct {
	mu     sync.Mutex
	ring   []Entry
	size   int
	nextID int64
	dir    string
	day    string
	file   *os.File
}

func New(dir string) *Logger {
	_ = os.MkdirAll(dir, 0o755)
	return &Logger{size: 3000, dir: dir}
}

func (l *Logger) write(level, scope, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextID++
	e := Entry{ID: l.nextID, Time: time.Now().Format("2006-01-02 15:04:05.000"), Level: level, Scope: scope, Msg: msg}
	l.ring = append(l.ring, e)
	if len(l.ring) > l.size {
		l.ring = l.ring[len(l.ring)-l.size:]
	}
	day := time.Now().Format("20060102")
	if l.day != day {
		if l.file != nil {
			_ = l.file.Close()
		}
		f, err := os.OpenFile(filepath.Join(l.dir, "app-"+day+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			l.file = f
			l.day = day
		}
	}
	if l.file != nil {
		_, _ = fmt.Fprintf(l.file, "%s [%s] [%s] %s\n", e.Time, level, scope, msg)
	}
}

func (l *Logger) Info(scope, format string, args ...any)  { l.write("INFO", scope, fmt.Sprintf(format, args...)) }
func (l *Logger) Warn(scope, format string, args ...any)  { l.write("WARN", scope, fmt.Sprintf(format, args...)) }
func (l *Logger) Error(scope, format string, args ...any) { l.write("ERROR", scope, fmt.Sprintf(format, args...)) }
func (l *Logger) Debug(scope, format string, args ...any) { l.write("DEBUG", scope, fmt.Sprintf(format, args...)) }

// List 返回 id 大于 after 的日志（最多 limit 条），level 为空表示全部
func (l *Logger) List(after int64, limit int, level string) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	out := make([]Entry, 0, limit)
	for _, e := range l.ring {
		if e.ID <= after {
			continue
		}
		if level != "" && !strings.EqualFold(e.Level, level) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out
}
