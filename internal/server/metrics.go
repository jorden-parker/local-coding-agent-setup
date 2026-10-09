package server

import (
	"bufio"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Metrics is a scrape of llama-server's Prometheus endpoint, keyed by the
// metric name without the llamacpp: prefix.
type Metrics map[string]float64

// Scrape fetches http://127.0.0.1:<port>/metrics (needs --metrics).
func Scrape(port int) (Metrics, error) {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/metrics returned %s (is llama-server running with --metrics?)", resp.Status)
	}
	m := Metrics{}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || !strings.HasPrefix(line, "llamacpp:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[0], "llamacpp:")
		if i := strings.IndexByte(name, '{'); i >= 0 {
			name = name[:i]
		}
		v, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		m[name] = v
	}
	return m, sc.Err()
}

// Health reports whether /health answers 200.
func Health(port int) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Summary renders the metrics people care about on one line.
func (m Metrics) Summary() string {
	get := func(k string) string {
		if v, ok := m[k]; ok {
			return strconv.FormatFloat(v, 'f', 1, 64)
		}
		return "-"
	}
	return fmt.Sprintf("prompt %s t/s  gen %s t/s  processing %s  deferred %s  kv %s%%",
		get("prompt_tokens_seconds"), get("predicted_tokens_seconds"),
		get("requests_processing"), get("requests_deferred"), get("kv_cache_usage_ratio"))
}
