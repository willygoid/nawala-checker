package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/l0rd/nawala-checker/internal/checker"
)

// App struct
type App struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
}

// CheckDomains parses the input text (one domain per line),
// and runs the checker in the background, streaming events to the frontend.
func (a *App) CheckDomains(input string, workers int, timeoutSec int, retries int) {
	if a.cancel != nil {
		a.cancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel

	jobs := checker.ParseLines(strings.Split(input, "\n"), nil)
	if len(jobs) == 0 {
		runtime.EventsEmit(ctx, "onFinished", checker.Summary{})
		return
	}

	client := checker.NewClient(time.Duration(timeoutSec)*time.Second, retries)

	// Emit initial state
	runtime.EventsEmit(ctx, "onStarted", len(jobs))

	go func() {
		summary := client.Run(ctx, jobs, workers, func(r checker.Result) {
			runtime.EventsEmit(ctx, "onResult", r)
		})
		runtime.EventsEmit(ctx, "onFinished", summary)
		a.cancel = nil
	}()
}

// StopCheck stops the checking process
func (a *App) StopCheck() {
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
}

// OpenFileDialog prompts the user to select a .txt file and returns its content
func (a *App) OpenFileDialog() (string, error) {
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Pilih File .txt",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "Text Files (*.txt)",
				Pattern:     "*.txt",
			},
		},
	})
	if err != nil || filePath == "" {
		return "", err
	}

	jobs, err := checker.LoadFiles([]string{filePath})
	if err != nil {
		return "", err
	}
	
	// Convert jobs back to newline separated string to feed into the UI textarea easily
	var domains []string
	for _, j := range jobs {
		domains = append(domains, j.Raw)
	}
	return strings.Join(domains, "\n"), nil
}

// SaveCSVDialog prompts the user to save a CSV file and writes the given content
func (a *App) SaveCSVDialog(csvContent string) error {
	filePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Simpan Hasil CSV",
		DefaultFilename: "hasil-nawala.csv",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "CSV Files (*.csv)",
				Pattern:     "*.csv",
			},
		},
	})
	if err != nil || filePath == "" {
		return err
	}

	return os.WriteFile(filePath, []byte(csvContent), 0644)
}

func (a *App) GetVersion() string {
	return Version
}

func (a *App) GetDeveloper() string {
	return Developer
}
