// Package cli menjalankan nawala-checker dalam mode terminal.
package cli

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/schollz/progressbar/v3"

	"github.com/l0rd/nawala-checker/internal/checker"
)

// warna ANSI
const (
	cReset  = "\033[0m"
	cRed    = "\033[31;1m"
	cGreen  = "\033[32;1m"
	cYellow = "\033[33;1m"
	cCyan   = "\033[36m"
	cDim    = "\033[2m"
)

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// Run mengeksekusi mode CLI. Return exit code.
func Run(args []string, version, developer string) int {
	fs := flag.NewFlagSet("nawala-checker", flag.ContinueOnError)
	var files multiFlag
	fs.Var(&files, "f", "file .txt berisi URL/domain (boleh dipakai berkali-kali: -f a.txt -f b.txt)")
	workers := fs.Int("w", 10, "jumlah worker concurrent")
	timeout := fs.Duration("t", 15*time.Second, "timeout HTTP per request")
	retries := fs.Int("r", 3, "jumlah retry per domain")
	outCSV := fs.String("o", "", "simpan hasil ke file CSV")
	outJSON := fs.String("json", "", "simpan hasil ke file JSON")
	noColor := fs.Bool("no-color", false, "matikan warna output")
	quiet := fs.Bool("q", false, "hanya tampilkan domain DIBLOKIR")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Nawala Checker v%s — cek status blokir TrustPositif (Komdigi/Kominfo)
Crafted by %s

Pemakaian:
  nawala-checker [opsi] [domain ...]
  nawala-checker -f domains.txt -f lainnya.txt -w 20
  cat domains.txt | nawala-checker
  nawala-checker gui        (atau jalankan tanpa argumen untuk mode GUI)

Domain boleh berupa URL lengkap (https://situs.com/path) atau domain polos.

Opsi:
`, version, developer)
		fs.PrintDefaults()
	}
	// Stdlib flag berhenti parse setelah argumen posisi pertama; reorder agar
	// flag boleh ditaruh di mana saja (nawala-checker domain.com -w 20 tetap jalan).
	boolFlags := map[string]bool{"no-color": true, "q": true}
	var flagArgs, posArgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			name := strings.TrimLeft(a, "-")
			if eq := strings.Index(name, "="); eq != -1 {
				name = name[:eq]
				flagArgs = append(flagArgs, a)
				continue
			}
			flagArgs = append(flagArgs, a)
			if !boolFlags[name] && i+1 < len(args) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
		} else {
			posArgs = append(posArgs, a)
		}
	}
	if err := fs.Parse(append(flagArgs, posArgs...)); err != nil {
		return 2
	}

	color := func(c, s string) string {
		if *noColor {
			return s
		}
		return c + s + cReset
	}

	// Kumpulkan domain: file → argumen posisi → stdin (jika dipipe).
	fileJobs, err := checker.LoadFiles(files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", color(cRed, "[ERROR]"), err)
		return 1
	}
	argJobs := checker.ParseLines(fs.Args(), nil)

	var stdinJobs []checker.Job
	if st, _ := os.Stdin.Stat(); st != nil && (st.Mode()&os.ModeCharDevice) == 0 {
		var lines []string
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		stdinJobs = checker.ParseLines(lines, nil)
	}

	jobs := checker.MergeJobs(fileJobs, argJobs, stdinJobs)
	if len(jobs) == 0 {
		fs.Usage()
		fmt.Fprintf(os.Stderr, "\n%s tidak ada domain untuk dicek\n", color(cYellow, "[!]"))
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := checker.NewClient(*timeout, *retries)

	fmt.Fprintf(os.Stderr, "%s mengecek %d domain dengan %d worker...\n\n",
		color(cCyan, "[*]"), len(jobs), *workers)

	bar := progressbar.NewOptions(len(jobs),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetDescription("Mengecek"),
		progressbar.OptionShowCount(),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer: "█", SaucerPadding: "░", BarStart: "|", BarEnd: "|",
		}),
		progressbar.OptionThrottle(50*time.Millisecond),
		progressbar.OptionSetRenderBlankState(true),
	)

	var mu sync.Mutex
	var results []checker.Result

	// onResult dipanggil sekuensial oleh Run — hasil tampil realtime di atas bar.
	summary := client.Run(ctx, jobs, *workers, func(r checker.Result) {
		mu.Lock()
		results = append(results, r)
		mu.Unlock()

		var line string
		switch r.Status {
		case checker.StatusBlocked:
			line = fmt.Sprintf("%s %s", color(cRed, "[DIBLOKIR]"), r.Domain)
		case checker.StatusNotBlocked:
			if *quiet {
				line = ""
			} else {
				line = fmt.Sprintf("%s %s", color(cGreen, "[AMAN]    "), r.Domain)
			}
		default:
			line = fmt.Sprintf("%s %s %s", color(cYellow, "[UNKNOWN] "), r.Domain,
				color(cDim, "("+r.Err+")"))
		}
		if line != "" {
			bar.Clear()
			fmt.Println(line)
		}
		bar.Add(1)
	})

	bar.Finish()
	fmt.Fprintln(os.Stderr)

	// Ringkasan
	fmt.Fprintf(os.Stderr, "\n%s Selesai dalam %s — total %d | %s %d | %s %d | %s %d\n",
		color(cCyan, "[*]"), summary.Elapsed.Round(time.Millisecond), summary.Total,
		color(cRed, "DIBLOKIR:"), summary.Blocked,
		color(cGreen, "AMAN:"), summary.NotBlocked,
		color(cYellow, "UNKNOWN:"), summary.Unknown)

	if ctx.Err() != nil {
		fmt.Fprintf(os.Stderr, "%s dihentikan sebelum selesai (%d/%d dicek)\n",
			color(cYellow, "[!]"), summary.Total, len(jobs))
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Index < results[j].Index })

	if *outCSV != "" {
		if err := writeCSV(*outCSV, results); err != nil {
			fmt.Fprintf(os.Stderr, "%s gagal tulis CSV: %v\n", color(cRed, "[ERROR]"), err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "%s hasil tersimpan: %s\n", color(cCyan, "[*]"), *outCSV)
	}
	if *outJSON != "" {
		if err := writeJSON(*outJSON, results); err != nil {
			fmt.Fprintf(os.Stderr, "%s gagal tulis JSON: %v\n", color(cRed, "[ERROR]"), err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "%s hasil tersimpan: %s\n", color(cCyan, "[*]"), *outJSON)
	}

	if summary.Unknown > 0 {
		return 3
	}
	return 0
}

func writeCSV(path string, results []checker.Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{"domain", "input_asli", "status", "error", "durasi_ms"})
	for _, r := range results {
		w.Write([]string{
			r.Domain, r.Raw, string(r.Status), r.Err,
			fmt.Sprintf("%d", r.Duration.Milliseconds()),
		})
	}
	return w.Error()
}

func writeJSON(path string, results []checker.Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}
