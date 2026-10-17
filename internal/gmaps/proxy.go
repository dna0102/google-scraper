package gmaps

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
)

var ErrLeak = errors.New("not hiding your real IP")

var schemes = map[string]bool{"http": true, "https": true, "socks4": true, "socks4a": true, "socks5": true, "socks5h": true}

type ProxyCheck struct {
	Proxy   string
	IP      string
	Latency time.Duration
	Err     error
}

func ParseProxy(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	scheme := "http"
	if before, after, ok := strings.Cut(s, "://"); ok {
		scheme, s = strings.ToLower(before), after
	}
	if !schemes[scheme] {
		return "", fmt.Errorf("unsupported scheme %q", scheme)
	}

	if at := strings.LastIndex(s, "@"); at >= 0 {
		if host, port, ok := splitHostPort(s[at+1:]); ok {
			user, pass, _ := strings.Cut(s[:at], ":")
			return proxyURL(scheme, host, port, user, pass), nil
		}
	}

	parts := strings.SplitN(s, ":", 4)
	if len(parts) != 2 && len(parts) != 4 {
		return "", fmt.Errorf("can't read %q", raw)
	}
	host, port, ok := splitHostPort(parts[0] + ":" + parts[1])
	if !ok {
		return "", fmt.Errorf("can't read %q", raw)
	}
	if len(parts) == 4 {
		return proxyURL(scheme, host, port, parts[2], parts[3]), nil
	}
	return proxyURL(scheme, host, port, "", ""), nil
}

func splitHostPort(s string) (host, port string, ok bool) {
	host, port, _ = strings.Cut(s, ":")
	n, err := strconv.Atoi(port)
	return host, port, host != "" && err == nil && n > 0 && n < 65536
}

func proxyURL(scheme, host, port, user, pass string) string {
	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port)}
	if user != "" {
		u.User = url.UserPassword(user, pass)
	}
	return u.String()
}

func Redact(proxy string) string {
	u, err := url.Parse(proxy)
	if err != nil || u.Host == "" {
		return proxy
	}
	return u.Scheme + "://" + u.Host
}

func LoadProxies(path string) (proxies, skipped []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		proxy, err := ParseProxy(text)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		proxies = append(proxies, proxy)
	}
	return proxies, skipped, sc.Err()
}

func PruneProxies(path string, alive map[string]bool) (removed int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	var keep []string
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if proxy, err := ParseProxy(line); err == nil && !alive[proxy] {
			removed++
			continue
		}
		keep = append(keep, line)
	}
	return removed, os.WriteFile(path, []byte(strings.Join(keep, "\n")), 0o644)
}

func PublicIP(ctx context.Context) string {
	hc, err := newHTTPClient("", 10*time.Second)
	if err != nil {
		return ""
	}
	return exitIP(ctx, hc)
}

func exitIP(ctx context.Context, hc tls_client.HttpClient) string {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	req.Header = http.Header{"user-agent": {userAgent}}

	resp, err := hc.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	if ip := net.ParseIP(strings.TrimSpace(string(body))); ip != nil {
		return ip.String()
	}
	return ""
}

func checkProxy(ctx context.Context, hc tls_client.HttpClient, proxy, realIP string) ProxyCheck {
	result := ProxyCheck{Proxy: proxy}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.google.com/generate_204", nil)
	req.Header = http.Header{"user-agent": {userAgent}}

	start := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		result.Err = err
		return result
	}
	resp.Body.Close()
	result.Latency = time.Since(start)

	if resp.StatusCode != http.StatusNoContent {
		result.Err = fmt.Errorf("Google answered HTTP %d", resp.StatusCode)
		return result
	}
	if result.IP = exitIP(ctx, hc); result.IP != "" && result.IP == realIP {
		result.Err = ErrLeak
	}
	return result
}

func checkAll(n, workers int, check func(i int) ProxyCheck, done func(ProxyCheck)) []ProxyCheck {
	results := make([]ProxyCheck, n)
	limit := make(chan struct{}, max(workers, 32))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			results[i] = check(i)
			<-limit

			if done != nil {
				mu.Lock()
				done(results[i])
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return results
}

func CheckProxies(ctx context.Context, proxies []string, workers int, timeout time.Duration, done func(ProxyCheck)) []ProxyCheck {
	realIP := PublicIP(ctx)
	return checkAll(len(proxies), workers, func(i int) ProxyCheck {
		hc, err := newHTTPClient(proxies[i], timeout)
		if err != nil {
			return ProxyCheck{Proxy: proxies[i], Err: err}
		}
		return checkProxy(ctx, hc, proxies[i], realIP)
	}, done)
}

func (c *Client) Verify(ctx context.Context) (*Client, []ProxyCheck) {
	realIP := PublicIP(ctx)
	checks := checkAll(len(c.cfg.Proxies), c.cfg.Parallel, func(i int) ProxyCheck {
		first := c.pool[i*c.perProxy]
		return checkProxy(ctx, first.http, first.proxy, realIP)
	}, nil)

	working := &Client{cfg: c.cfg, perProxy: c.perProxy, sem: make(chan struct{}, c.cfg.Parallel), language: c.language}
	working.cfg.Proxies = nil
	for i, check := range checks {
		if check.Err == nil {
			working.cfg.Proxies = append(working.cfg.Proxies, check.Proxy)
			working.pool = append(working.pool, c.pool[i*c.perProxy:(i+1)*c.perProxy]...)
		}
	}
	return working, checks
}
