package collector

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"collect/internal/config"
	"collect/internal/model"
)

// defaultUserAgent 接口采集默认 UA：优先使用「设置 → 浏览器指纹」里配置的 UA，
// 保证纯接口模式与浏览器里看到的 UA 一致，避免 UA 不匹配触发风控。
func defaultUserAgent() string {
	if ua := strings.TrimSpace(config.Get().Browser.UserAgent); ua != "" {
		return ua
	}
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
}

type HTTPResult struct {
	Status     int
	Headers    map[string]string
	Body       []byte
	Text       string
	JSON       any
	Duration   time.Duration
	Size       int
	ReqURL     string
	ReqBody    string
	ReqHeaders map[string]string
}

type Executor struct {
	client *http.Client
	jar    http.CookieJar
}

func NewExecutor(timeout time.Duration, proxy string, insecure bool) (*Executor, error) {
	jar, _ := cookiejar.New(nil)
	return NewExecutorWithJar(timeout, proxy, insecure, jar)
}

// NewExecutorWithJar 使用指定的 CookieJar 创建执行器。
// 同一作业内的多个 HTTP 步骤共用同一个 Jar，即可保持登录会话。
func NewExecutorWithJar(timeout time.Duration, proxy string, insecure bool, jar http.CookieJar) (*Executor, error) {
	if jar == nil {
		jar, _ = cookiejar.New(nil)
	}
	tr := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: insecure}, //nolint:gosec
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     60 * time.Second,
	}
	if proxy != "" {
		pu, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("代理地址无效: %w", err)
		}
		tr.Proxy = http.ProxyURL(pu)
	}
	return &Executor{
		client: &http.Client{Timeout: timeout, Transport: tr, Jar: jar},
		jar:    jar,
	}, nil
}

func (x *Executor) Jar() http.CookieJar { return x.jar }

func (x *Executor) Do(method, rawURL string, headers []model.KV, body, contentType string) (*HTTPResult, error) {
	return x.DoContext(context.Background(), method, rawURL, headers, body, contentType)
}

func (x *Executor) DoContext(ctx context.Context, method, rawURL string, headers []model.KV, body, contentType string) (*HTTPResult, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	for _, h := range headers {
		if strings.TrimSpace(h.Key) == "" {
			continue
		}
		req.Header.Set(strings.TrimSpace(h.Key), h.Value)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", defaultUserAgent())
	}
	if req.Header.Get("Accept-Language") == "" {
		if lang := strings.TrimSpace(config.Get().Browser.AcceptLanguage); lang != "" {
			req.Header.Set("Accept-Language", lang)
		}
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json, text/plain, */*")
	}
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	res := &HTTPResult{
		ReqURL: rawURL, ReqBody: body, ReqHeaders: headerStringMap(req.Header),
	}
	start := time.Now()
	resp, err := x.client.Do(req)
	if err != nil {
		return res, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return res, fmt.Errorf("读取响应失败: %w", err)
	}
	res.Status = resp.StatusCode
	res.Headers = map[string]string{}
	res.Body = raw
	res.Text = string(raw)
	res.Duration = time.Since(start)
	res.Size = len(raw)
	for k, vals := range resp.Header {
		res.Headers[k] = strings.Join(vals, "; ")
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "json") || looksJSON(raw) {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			res.JSON = v
		}
	}
	if resp.StatusCode >= 400 {
		return res, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateText(res.Text, 300))
	}
	return res, nil
}

func headerStringMap(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vals := range h {
		out[k] = strings.Join(vals, "; ")
	}
	return out
}

func looksJSON(b []byte) bool {
	t := strings.TrimSpace(string(b))
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

func truncateText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// CheckConn 测试数据库连接
func CheckConn(ds model.DataSource, timeout time.Duration) error {
	db, err := OpenSQL(ds)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return errors.New("连接失败: " + err.Error())
	}
	return nil
}
