package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

const resultsDir = "results"

var (
	accent = lipgloss.AdaptiveColor{Light: "#5A56E0", Dark: "#8B87FF"}
	muted  = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#8B949E"}

	logo  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Padding(0, 1)
	faint = lipgloss.NewStyle().Foreground(muted)
	good  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3FB950"))
	bad   = lipgloss.NewStyle().Foreground(lipgloss.Color("#F85149"))
	warn  = lipgloss.NewStyle().Foreground(lipgloss.Color("#D29922"))
	bold  = lipgloss.NewStyle().Bold(true)
	box   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 3)

	tty = term.IsTerminal(os.Stdout.Fd())
)

func form(groups ...*huh.Group) error {
	return huh.NewForm(groups...).WithTheme(huh.ThemeCharm()).Run()
}

func ask(fields ...huh.Field) error {
	return form(huh.NewGroup(fields...))
}

func enterScreen() func() {
	if !tty {
		return func() {}
	}
	fmt.Print("\033[?1049h\033[H\033[2J")
	return func() { fmt.Print("\033[?25h\033[?1049l") }
}

func header(status string) {
	if tty {
		fmt.Print("\033[H\033[2J\033[3J")
	}
	fmt.Printf("\n  %s  %s\n  %s\n\n", logo.Render("GScraper"), faint.Render("Google Maps lead scraper"), faint.Render(status))
}

func pause() {
	ask(huh.NewSelect[int]().Options(huh.NewOption("Back to the menu", 0)))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func required(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("this can't be empty")
	}
	return nil
}

func fileExists(s string) error {
	if st, err := os.Stat(strings.Trim(strings.TrimSpace(s), `"'`)); err != nil || st.IsDir() {
		return errors.New("file not found")
	}
	return nil
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

func BatchName(path string) string {
	base := filepath.Base(path)
	return "batch_" + strings.TrimSuffix(base, filepath.Ext(base))
}

func OutputPath(name, ext string) string {
	slug := strings.Trim(nonWord.ReplaceAllString(strings.ToLower(name), "_"), "_")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "_")
	}
	return filepath.Join(resultsDir, slug+"_"+time.Now().Format("20060102_150405")+"."+ext)
}

func openPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", abs).Start()
	case "darwin":
		return exec.Command("open", abs).Start()
	default:
		return exec.Command("xdg-open", abs).Start()
	}
}

func afterSave(path string) {
	for {
		choice := "back"
		err := ask(huh.NewSelect[string]().Title("What next?").Options(
			huh.NewOption("Open the file", "file"),
			huh.NewOption("Open the results folder", "folder"),
			huh.NewOption("Back to the menu", "back"),
		).Value(&choice))
		if err != nil || choice == "back" {
			return
		}
		target := path
		if choice == "folder" {
			target = filepath.Dir(path)
		}
		if err := openPath(target); err != nil {
			fmt.Println(bad.Render("  couldn't open it: " + err.Error()))
		}
	}
}

func ago(t time.Time) string {
	switch d := time.Since(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return t.Format("2 Jan 15:04")
	}
}
