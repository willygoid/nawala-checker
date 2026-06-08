// Package checker berisi inti logika pengecekan domain ke TrustPositif (Nawala).
package checker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Status hasil pengecekan sebuah domain.
type Status string

const (
	StatusBlocked    Status = "DIBLOKIR"  // TrustPositif: "Ada"
	StatusNotBlocked Status = "AMAN"      // TrustPositif: "Tidak Ada"
	StatusUnknown    Status = "UNKNOWN"   // lookup gagal setelah retry
)

// Result adalah hasil pengecekan satu domain.
type Result struct {
	Index    int           `json:"index"`
	Domain   string        `json:"domain"`
	Raw      string        `json:"raw"`    // input asli sebelum dinormalisasi
	Status   Status        `json:"status"`
	Err      string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

// Summary ringkasan akhir.
type Summary struct {
	Total      int
	Blocked    int
	NotBlocked int
	Unknown    int
	Elapsed    time.Duration
}

// DefaultEndpoints: dicoba berurutan (komdigi adalah domain baru kominfo).
var DefaultEndpoints = []string{
	"https://trustpositif.komdigi.go.id/Rest_server/getrecordsname_home",
	"https://trustpositif.kominfo.go.id/Rest_server/getrecordsname_home",
}

// Client melakukan lookup ke API TrustPositif.
type Client struct {
	HTTP      *http.Client
	Endpoints []string
	Retries   int           // percobaan ulang per domain (total attempt = Retries+1)
	RetryWait time.Duration // jeda antar retry
	UserAgent string
}

// NewClient membuat client dengan nilai default yang wajar.
func NewClient(timeout time.Duration, retries int) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: timeout},
		Endpoints: DefaultEndpoints,
		Retries:   retries,
		RetryWait: time.Second,
		UserAgent: "Mozilla/5.0 (compatible; nawala-checker/1.0)",
	}
}

type apiResponse struct {
	Values []struct {
		Domain string `json:"Domain"`
		Status string `json:"Status"`
	} `json:"values"`
}

// Check memeriksa satu domain. Aman dipanggil concurrent.
func (c *Client) Check(ctx context.Context, domain string) (Status, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return StatusUnknown, ctx.Err()
			case <-time.After(c.RetryWait):
			}
		}
		for _, ep := range c.Endpoints {
			st, err := c.lookup(ctx, ep, domain)
			if err == nil {
				return st, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return StatusUnknown, ctx.Err()
			}
		}
	}
	return StatusUnknown, lastErr
}

func (c *Client) lookup(ctx context.Context, endpoint, domain string) (Status, error) {
	form := url.Values{"name": {domain}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return StatusUnknown, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return StatusUnknown, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return StatusUnknown, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return StatusUnknown, err
	}
	var data apiResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return StatusUnknown, fmt.Errorf("invalid JSON: %w", err)
	}
	for _, v := range data.Values {
		if strings.EqualFold(strings.TrimSpace(v.Status), "ada") {
			return StatusBlocked, nil
		}
	}
	return StatusNotBlocked, nil
}

// Job adalah satu unit pekerjaan (domain ternormalisasi + input asli).
type Job struct {
	Index  int
	Domain string
	Raw    string
}

// Run menjalankan pengecekan dengan worker pool. onResult dipanggil secara
// berurutan (single goroutine) setiap satu hasil selesai — aman untuk update
// UI/progress bar tanpa lock tambahan. Blocking sampai semua selesai atau ctx batal.
func (c *Client) Run(ctx context.Context, jobs []Job, workers int, onResult func(Result)) Summary {
	if workers < 1 {
		workers = 1
	}
	if workers > len(jobs) && len(jobs) > 0 {
		workers = len(jobs)
	}
	start := time.Now()

	jobCh := make(chan Job)
	resCh := make(chan Result)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				t0 := time.Now()
				st, err := c.Check(ctx, job.Domain)
				r := Result{
					Index:    job.Index,
					Domain:   job.Domain,
					Raw:      job.Raw,
					Status:   st,
					Duration: time.Since(t0),
				}
				if err != nil {
					r.Err = err.Error()
				}
				select {
				case resCh <- r:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobCh)
		for _, j := range jobs {
			select {
			case jobCh <- j:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(resCh)
	}()

	var sum Summary
	for r := range resCh {
		sum.Total++
		switch r.Status {
		case StatusBlocked:
			sum.Blocked++
		case StatusNotBlocked:
			sum.NotBlocked++
		default:
			sum.Unknown++
		}
		if onResult != nil {
			onResult(r)
		}
	}
	sum.Elapsed = time.Since(start)
	return sum
}

// NormalizeDomain mengubah satu baris input (URL dengan/tanpa schema, dengan
// path/port/kredensial) menjadi hostname bersih. ok=false jika baris kosong/komentar.
func NormalizeDomain(line string) (string, bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, "-") {
		return "", false
	}
	// Buang schema apa pun (http://, https://, ftp://, dst).
	if i := strings.Index(s, "://"); i != -1 {
		s = s[i+3:]
	}
	// Buang kredensial user:pass@
	if i := strings.LastIndex(s, "@"); i != -1 {
		s = s[i+1:]
	}
	// Buang path, query, fragment.
	for _, sep := range []string{"/", "?", "#"} {
		if i := strings.Index(s, sep); i != -1 {
			s = s[:i]
		}
	}
	// Buang port.
	if i := strings.LastIndex(s, ":"); i != -1 && !strings.Contains(s, "]") {
		s = s[:i]
	}
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
	s = strings.TrimPrefix(s, "[") // IPv6 literal
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return "", false
	}
	return s, true
}

// ParseLines menormalisasi banyak baris sekaligus, dedupe, urutan terjaga.
func ParseLines(lines []string, seen map[string]bool) []Job {
	if seen == nil {
		seen = map[string]bool{}
	}
	var jobs []Job
	for _, ln := range lines {
		d, ok := NormalizeDomain(ln)
		if !ok || seen[d] {
			continue
		}
		seen[d] = true
		jobs = append(jobs, Job{Index: len(jobs), Domain: d, Raw: strings.TrimSpace(ln)})
	}
	return jobs
}

// LoadFiles membaca beberapa file .txt (satu URL/domain per baris),
// dinormalisasi dan di-dedupe lintas file.
func LoadFiles(paths []string) ([]Job, error) {
	seen := map[string]bool{}
	var jobs []Job
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("buka %s: %w", p, err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			d, ok := NormalizeDomain(sc.Text())
			if !ok || seen[d] {
				continue
			}
			seen[d] = true
			jobs = append(jobs, Job{Index: len(jobs), Domain: d, Raw: strings.TrimSpace(sc.Text())})
		}
		errScan := sc.Err()
		f.Close()
		if errScan != nil {
			return nil, fmt.Errorf("baca %s: %w", p, errScan)
		}
	}
	// Re-index setelah dedupe lintas file.
	for i := range jobs {
		jobs[i].Index = i
	}
	return jobs, nil
}

// MergeJobs menggabungkan beberapa sumber job dengan dedupe.
func MergeJobs(sources ...[]Job) []Job {
	seen := map[string]bool{}
	var out []Job
	for _, src := range sources {
		for _, j := range src {
			if seen[j.Domain] {
				continue
			}
			seen[j.Domain] = true
			j.Index = len(out)
			out = append(out, j)
		}
	}
	return out
}
