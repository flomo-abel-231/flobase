// Command flobase runs FloBase on your computer, on top of Docker Desktop,
// OrbStack, Rancher Desktop or Docker Engine.
//
//	flobase setup      install what FloBase needs, including a Docker engine
//	flobase start      set up on first use, start, and open the dashboard
//	flobase stop       stop FloBase and its project databases; data is kept
//	flobase status     show whether it runs, its version and URLs
//	flobase open       open the dashboard, signed in
//	flobase logs       show recent control-plane and gateway logs
//	flobase update     move to the latest FloBase release
//	flobase uninstall  remove FloBase (add --all to delete every project too)
//	flobase version    print this program's version
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"flobase/control-plane/local"
)

var version = "dev"

const usage = `flobase runs FloBase on your computer.

Usage:
  flobase setup               Install what FloBase needs, including a Docker engine
  flobase start [--no-open]   Set up on first use, start, and open the dashboard
  flobase stop                Stop FloBase and its project databases (data is kept)
  flobase status              Show whether FloBase runs, its version and URLs
  flobase open                Open the dashboard, signed in
  flobase logs [--lines N]    Show recent logs
  flobase update              Update to the latest FloBase release
  flobase uninstall [--all]   Remove FloBase; --all also deletes every project and its data
  flobase version             Print this program's version

FloBase runs on a Docker engine. It uses the one on this computer, or
installs one: its own engine on macOS (no admin rights needed), Docker
Desktop with WSL 2 on Windows, Docker Engine on Linux. Files live in
~/.flobase (FLOBASE_LOCAL_DIR). FLOBASE_YES=1 answers yes to every question.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "\033[31mError:\033[0m", err)
		var noDocker *local.ErrNoDocker
		var restart *local.ErrRestartRequired
		switch {
		case errors.As(err, &noDocker):
			os.Exit(3)
		case errors.As(err, &restart):
			os.Exit(4)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd string, args []string) error {
	m, err := local.New(version)
	if err != nil {
		return err
	}
	m.Progress = func(line string) { fmt.Println("\033[33m==>\033[0m", line) }
	m.Confirm = func(question string) bool {
		if os.Getenv("FLOBASE_YES") == "1" {
			return true
		}
		return confirm(question + " [y/N] ")
	}

	switch cmd {
	case "setup":
		if err := m.EnsureEngine(ctx); err != nil {
			return err
		}
		fmt.Println("Everything FloBase needs is installed. Run: flobase start")
	case "start":
		fs := flag.NewFlagSet("start", flag.ExitOnError)
		noOpen := fs.Bool("no-open", false, "do not open the dashboard")
		_ = fs.Parse(args)
		if err := m.Start(ctx); err != nil {
			return err
		}
		printURLs(m, m.Status(ctx))
		if !*noOpen {
			return local.OpenURL(m.DashboardURL(true))
		}
	case "stop":
		return m.Stop(ctx)
	case "status":
		s := m.Status(ctx)
		switch {
		case !s.Installed:
			fmt.Println("FloBase is not set up yet. Run: flobase start")
		case s.Running:
			fmt.Printf("FloBase %s is running.\n", s.Version)
			printURLs(m, s)
		default:
			fmt.Printf("FloBase %s is stopped. Run: flobase start\n", s.Version)
		}
	case "open":
		if !m.Status(ctx).Running {
			return errors.New("FloBase is not running. Run: flobase start")
		}
		return local.OpenURL(m.DashboardURL(true))
	case "logs":
		fs := flag.NewFlagSet("logs", flag.ExitOnError)
		lines := fs.Int("lines", 100, "how many lines")
		_ = fs.Parse(args)
		out, err := m.Logs(ctx, *lines)
		fmt.Print(out)
		return err
	case "update":
		_, err := m.Update(ctx)
		return err
	case "uninstall":
		fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
		all := fs.Bool("all", false, "also delete every project and all data")
		yes := fs.Bool("yes", false, "do not ask for confirmation")
		_ = fs.Parse(args)
		if *all && !*yes && !confirm("This deletes every local project and all their data. Type yes to continue: ") {
			return errors.New("cancelled")
		}
		return m.Uninstall(ctx, *all)
	case "version", "--version", "-v":
		fmt.Println("flobase", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
	return nil
}

func printURLs(m *local.Manager, s local.Status) {
	fmt.Printf(`
  Dashboard     %s
  Project APIs  %s
  API token     %s
  Files         %s

`, s.DashboardURL, s.GatewayURL, m.Env()["FLOBASE_API_TOKEN"], s.Dir)
}

// confirm accepts "yes", or "y" for [y/N] questions.
func confirm(prompt string) bool {
	fmt.Print(prompt)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "yes" || (strings.HasSuffix(prompt, "[y/N] ") && answer == "y")
}
