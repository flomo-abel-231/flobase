package main

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"flobase/control-plane/local"
)

// App is bound to the window's JavaScript as window.go.main.App. Long
// actions run in the background and report through the "progress",
// "status" and "failed" events.
type App struct {
	ctx  context.Context
	m    *local.Manager
	mu   sync.Mutex
	busy bool
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	m, err := local.New(version)
	if err != nil {
		wruntime.EventsEmit(ctx, "failed", err.Error())
		return
	}
	m.Progress = func(line string) { wruntime.EventsEmit(ctx, "progress", line) }
	m.Confirm = func(question string) bool {
		answer, err := wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{
			Type: wruntime.QuestionDialog, Title: "FloBase", Message: question,
			Buttons: []string{"Install", "Not now"}, DefaultButton: "Install", CancelButton: "Not now",
		})
		return err == nil && (answer == "Install" || answer == "Yes" || answer == "Ok")
	}
	a.m = m
	// Start FloBase as soon as the app opens, as a desktop app is expected to.
	a.Start()
}

// Version is this app's release.
func (a *App) Version() string { return version }

// Status reports whether FloBase runs, and where.
func (a *App) Status() local.Status {
	if a.m == nil {
		return local.Status{}
	}
	return a.m.Status(a.ctx)
}

// run does one long action at a time in the background.
func (a *App) run(name string, fn func() error) {
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		return
	}
	a.busy = true
	a.mu.Unlock()
	wruntime.EventsEmit(a.ctx, "busy", name)
	go func() {
		err := fn()
		a.mu.Lock()
		a.busy = false
		a.mu.Unlock()
		if err != nil {
			var restart *local.ErrRestartRequired
			if errors.As(err, &restart) {
				wruntime.EventsEmit(a.ctx, "restart", err.Error())
			} else {
				wruntime.EventsEmit(a.ctx, "failed", err.Error())
			}
		}
		wruntime.EventsEmit(a.ctx, "status", a.Status())
	}()
}

// Start installs what is missing, starts FloBase and opens the dashboard
// the first time it comes up.
func (a *App) Start() {
	a.run("start", func() error {
		if err := a.m.Start(a.ctx); err != nil {
			return err
		}
		return local.OpenURL(a.m.DashboardURL(true))
	})
}

// Stop stops FloBase and its project databases; data is kept.
func (a *App) Stop() { a.run("stop", func() error { return a.m.Stop(a.ctx) }) }

// Update moves to the latest FloBase release.
func (a *App) Update() {
	a.run("update", func() error {
		_, err := a.m.Update(a.ctx)
		return err
	})
}

// OpenDashboard opens the dashboard in the browser, signed in.
func (a *App) OpenDashboard() error { return local.OpenURL(a.m.DashboardURL(true)) }

// OpenURL opens a link from the window in the browser.
func (a *App) OpenURL(u string) error { return local.OpenURL(u) }

// Token returns the API token, for copying.
func (a *App) Token() string { return a.m.Env()["FLOBASE_API_TOKEN"] }

// OpenFolder shows FloBase's files in Finder or Explorer.
func (a *App) OpenFolder() error {
	cmd := exec.Command("open", a.m.Dir)
	if runtime.GOOS == "windows" {
		cmd = exec.Command("explorer", a.m.Dir)
	}
	return cmd.Start()
}

// Logs returns FloBase's recent logs.
func (a *App) Logs() (string, error) { return a.m.Logs(a.ctx, 200) }
