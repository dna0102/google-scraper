package gmaps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

const (
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"
	secChUa   = `"Google Chrome";v="152", "Chromium";v="152", "Not(A:Brand";v="24"`
	consent   = "CAESEwgDEgk0ODE3Nzk3MjQaAmVuIAEaBgiA_LyaBg"
	xssi      = ")]}'"

	streamsPerConn = 8
)

var ErrBlocked = errors.New("blocked by Google, slow down or use proxies")

var headerOrder = []string{"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform", "user-agent", "accept", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-dest", "referer", "accept-encoding", "accept-language", "cookie", "priority"}

type Config struct {
	Language string
	Region   string
	Proxies  []string
	Timeout  time.Duration
	Parallel int
	Retries  int
}

type conn struct {
	http  tls_client.HttpClient
	proxy string
}

type Client struct {
	cfg      Config
	pool     []conn
	perProxy int
	next     atomic.Uint64
	sem      chan struct{}
	language string
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.Language == "" {
		cfg.Language = "en"
	}
	if cfg.Region == "" {
		cfg.Region = "uk"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	cfg.Parallel = max(cfg.Parallel, 1)

	proxies := cfg.Proxies
	if len(proxies) == 0 {
		proxies = []string{""}
	}
	perProxy := max(1, (cfg.Parallel+streamsPerConn*len(proxies)-1)/(streamsPerConn*len(proxies)))
	c := &Client{cfg: cfg, perProxy: perProxy, sem: make(chan struct{}, cfg.Parallel), language: acceptLanguage(cfg.Language, cfg.Region)}

	for _, proxy := range proxies {
		for range perProxy {
			hc, err := newHTTPClient(proxy, cfg.Timeout)
			if err != nil {
				return nil, fmt.Errorf("proxy %s: %w", Redact(proxy), err)
			}
			c.pool = append(c.pool, conn{hc, proxy})
		}
	}
	return c, nil
}

func newHTTPClient(proxy string, timeout time.Duration) (tls_client.HttpClient, error) {
	jar := tls_client.NewCookieJar()
	options := []tls_client.HttpClientOption{
		tls_client.WithClientProfile(profiles.Chrome_152),
		tls_client.WithRandomTLSExtensionOrder(),
		tls_client.WithTimeoutSeconds(int(timeout.Seconds())),
		tls_client.WithCookieJar(jar),
		tls_client.WithNotFollowRedirects(),
	}
	if proxy != "" {
		options = append(options, tls_client.WithProxyUrl(proxy))
	}

	hc, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, err
	}
	google, _ := url.Parse("https://www.google.com/")
	jar.SetCookies(google, []*http.Cookie{{Name: "SOCS", Value: consent, Domain: ".google.com", Path: "/", Secure: true}})
	return hc, nil
}

func acceptLanguage(language, region string) string {
	region = strings.ToUpper(region)
	if region == "UK" {
		region = "GB"
	}
	return fmt.Sprintf("%s-%s,%s;q=0.9", language, region, language)
}

func (c *Client) Parallel() int   { return c.cfg.Parallel }
func (c *Client) ProxyCount() int { return len(c.cfg.Proxies) }

func (c *Client) nextConn() tls_client.HttpClient {
	n := c.next.Add(1) - 1
	return c.pool[n%uint64(len(c.pool))].http
}

func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			if err := c.backoff(ctx, attempt, lastErr); err != nil {
				return nil, err
			}
		}

		body, retry, err := c.fetch(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lastErr = err
		if !retry {
			break
		}
	}
	return nil, lastErr
}

func (c *Client) backoff(ctx context.Context, attempt int, lastErr error) error {
	wait := time.Duration(1<<(attempt-1)) * time.Second
	if errors.Is(lastErr, ErrBlocked) && c.ProxyCount() <= 1 {
		wait *= 5
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait + rand.N(wait/2)):
		return nil
	}
}

func (c *Client) fetch(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
	defer func() { <-c.sem }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header = http.Header{"sec-ch-ua": {secChUa}, "sec-ch-ua-mobile": {"?0"}, "sec-ch-ua-platform": {`"Windows"`}, "user-agent": {userAgent}, "accept": {"*/*"}, "sec-fetch-site": {"same-origin"}, "sec-fetch-mode": {"cors"}, "sec-fetch-dest": {"empty"}, "referer": {"https://www.google.com/maps/"}, "accept-encoding": {"gzip, deflate, br, zstd"}, "accept-language": {c.language}, "priority": {"u=1, i"}, http.HeaderOrderKey: headerOrder}

	resp, err := c.nextConn().Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()

	if body, err = io.ReadAll(resp.Body); err != nil {
		return nil, true, err
	}

	switch status := resp.StatusCode; {
	case status == http.StatusTooManyRequests, status == http.StatusFound && strings.Contains(resp.Header.Get("Location"), "/sorry/"):
		return nil, true, ErrBlocked
	case status == http.StatusProxyAuthRequired:
		return nil, true, errors.New("proxy rejected the credentials")
	case status >= 500:
		return nil, true, fmt.Errorf("HTTP %d", status)
	case status != http.StatusOK:
		return nil, false, fmt.Errorf("HTTP %d", status)
	case !bytes.HasPrefix(body, []byte(xssi)):
		return nil, true, errors.New("unexpected response from Google")
	}
	return body, false, nil
}
