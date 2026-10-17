package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"gscraper/internal/config"
	"gscraper/internal/gmaps"
	"gscraper/internal/output"
)

var proxyFormats = []string{"host:port", "host:port:user:pass", "user:pass@host:port", "http://host:port:user:pass", "http://user:pass@host:port", "socks5://host:port:user:pass", "socks5://user:pass@host:port"}

func (a *App) browse() {
	header(a.status())
	var files []string
	for _, ext := range []string{"csv", "xlsx", "json"} {
		matches, _ := filepath.Glob(filepath.Join(resultsDir, "*."+ext))
		files = append(files, matches...)
	}
	if len(files) == 0 {
		fmt.Println(faint.Render("  Nothing here yet. Run a search first.\n"))
		pause()
		return
	}

	info := map[string]os.FileInfo{}
	for _, f := range files {
		info[f], _ = os.Stat(f)
	}
	sort.Slice(files, func(i, j int) bool { return info[files[i]].ModTime().After(info[files[j]].ModTime()) })

	var options []huh.Option[string]
	for _, f := range files {
		details := fmt.Sprintf("%.0f KB · %s", float64(info[f].Size())/1024, ago(info[f].ModTime()))
		options = append(options, huh.NewOption(filepath.Base(f)+"  "+faint.Render(details), f))
	}

	var path string
	if ask(huh.NewSelect[string]().Title("Pick a file").Options(options...).Height(min(len(options)+2, 14)).Value(&path)) != nil {
		return
	}
	if strings.HasSuffix(path, ".csv") {
		header(a.status())
		if err := preview(path); err != nil {
			fmt.Println(bad.Render("  " + err.Error()))
		}
	}
	afterSave(path)
}

func preview(path string) error {
	header, rows, err := output.ReadCSV(path)
	if err != nil {
		return err
	}
	column := func(name string) func([]string) string {
		i := slices.Index(header, name)
		return func(row []string) string {
			if i < 0 || i >= len(row) {
				return ""
			}
			return row[i]
		}
	}
	name, phone, website, rating, reviews := column("name"), column("phone_number"), column("website"), column("reviews_average"), column("reviews_count")

	var withWebsite, withPhone int
	for _, r := range rows {
		if website(r) != "" {
			withWebsite++
		}
		if phone(r) != "" {
			withPhone++
		}
	}
	fmt.Printf("  %s  %s\n\n", bold.Render(filepath.Base(path)), faint.Render(fmt.Sprintf("%d businesses · %d with website · %d with phone", len(rows), withWebsite, withPhone)))

	t := table.New().Border(lipgloss.RoundedBorder()).BorderStyle(lipgloss.NewStyle().Foreground(muted)).Headers("Name", "Phone", "Website", "Rating")
	t.StyleFunc(func(row, _ int) lipgloss.Style {
		if row == table.HeaderRow {
			return bold.Padding(0, 1)
		}
		return lipgloss.NewStyle().Padding(0, 1)
	})
	for _, r := range rows[:min(15, len(rows))] {
		stars := rating(r)
		if stars != "" {
			stars += "★ (" + reviews(r) + ")"
		}
		t.Row(truncate(name(r), 36), phone(r), truncate(website(r), 30), stars)
	}

	fmt.Println(lipgloss.NewStyle().MarginLeft(2).Render(t.Render()))
	if len(rows) > 15 {
		fmt.Println(faint.Render(fmt.Sprintf("  …and %d more", len(rows)-15)))
	}
	fmt.Println()
	return nil
}

func (a *App) proxyMenu() {
	for {
		header(a.status())
		file := a.cfg.ProxyFile
		if file == "" {
			file = "proxies.txt"
		}

		if len(a.proxies) == 0 {
			fmt.Printf("  No proxies loaded. Add one per line to %s, any of these work:\n\n", bold.Render(file))
			for _, format := range proxyFormats {
				fmt.Println(faint.Render("    " + format))
			}
			fmt.Println()
		} else {
			fmt.Printf("  %s loaded from %s, rotated on every request\n\n", bold.Render(fmt.Sprintf("%d %s", len(a.proxies), plural(len(a.proxies), "proxy", "proxies"))), file)
		}
		for _, line := range a.skipped[:min(5, len(a.skipped))] {
			fmt.Println(warn.Render("  skipped " + line))
		}
		if len(a.skipped) > 0 {
			fmt.Println()
		}

		var options []huh.Option[string]
		if len(a.proxies) > 0 {
			options = append(options, huh.NewOption("Test them", "test"))
		}
		options = append(options, huh.NewOption("Edit "+file, "edit"), huh.NewOption("Reload the file", "reload"), huh.NewOption("Back", "back"))

		choice := options[0].Value
		if ask(huh.NewSelect[string]().Title("Proxies").Options(options...).Value(&choice)) != nil || choice == "back" {
			return
		}

		switch choice {
		case "test":
			a.testProxies(file)
		case "edit":
			a.editProxies(file)
		case "reload":
			a.reload()
		}
	}
}

func (a *App) editProxies(file string) {
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		os.WriteFile(file, nil, 0o644)
	}
	if a.cfg.ProxyFile == "" {
		a.cfg.ProxyFile = file
		a.cfg.Save()
	}
	openPath(file)
	ask(huh.NewSelect[int]().Title("Save the file, then come back here").Options(huh.NewOption("Done, reload it", 0)))
	a.reload()
}

func (a *App) testProxies(file string) {
	header(a.status())
	fmt.Printf("  Testing %d %s against google.com…\n\n", len(a.proxies), plural(len(a.proxies), "proxy", "proxies"))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	bar := newProgress(len(a.proxies), "tested")
	alive := map[string]bool{}
	gmaps.CheckProxies(ctx, a.proxies, a.cfg.Parallel, 15*time.Second, func(c gmaps.ProxyCheck) {
		bar.done++
		if c.Err != nil {
			bar.println(fmt.Sprintf("  %s %s  %s", bad.Render("✗"), gmaps.Redact(c.Proxy), faint.Render(shortErr(c.Err))))
			return
		}
		alive[c.Proxy] = true
		info := c.Latency.Round(time.Millisecond).String()
		if c.IP != "" {
			info += " · exit IP " + c.IP
		}
		bar.println(fmt.Sprintf("  %s %s  %s", good.Render("✓"), gmaps.Redact(c.Proxy), faint.Render(info)))
	})
	bar.finish()

	dead := len(a.proxies) - len(alive)
	fmt.Printf("\n  %s\n\n", bold.Render(fmt.Sprintf("%d of %d working", len(alive), len(a.proxies))))
	if dead == 0 {
		pause()
		return
	}

	remove := false
	ask(huh.NewConfirm().Title(fmt.Sprintf("Remove the %d dead ones from %s?", dead, file)).Value(&remove))
	if !remove {
		return
	}
	if n, err := gmaps.PruneProxies(file, alive); err != nil {
		a.notice = bad.Render(err.Error())
	} else {
		a.notice = good.Render(fmt.Sprintf("Removed %d dead proxies", n))
	}
	a.reload()
}

func shortErr(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && len(msg)-i > 8 {
		msg = msg[i+2:]
	}
	return truncate(msg, 60)
}

func (a *App) settings() {
	header(a.status())
	cfg := a.cfg

	var mustHave []string
	if cfg.Filters.RequireWebsite {
		mustHave = append(mustHave, "website")
	}
	if cfg.Filters.RequirePhone {
		mustHave = append(mustHave, "phone")
	}
	if cfg.Filters.RequireAddress {
		mustHave = append(mustHave, "address")
	}
	parallel := strconv.Itoa(cfg.Parallel)
	rating := strconv.FormatFloat(cfg.Filters.MinRating, 'f', -1, 64)
	reviews := strconv.Itoa(cfg.Filters.MinReviews)

	general := huh.NewGroup(
		huh.NewInput().Title("Parallel requests").Description("Any number from 1 to 1000. Higher is faster, without proxies 8 is a safe limit").Value(&parallel).Validate(wholeNumberBetween(1, 1000)),
		huh.NewSelect[bool]().Title("Columns to save").Options(
			huh.NewOption("Standard  "+faint.Render(strings.Join(output.Columns(false), ", ")), false),
			huh.NewOption("Everything  "+faint.Render("adds place type, store options, coordinates, ids, map link, query"), true),
		).Value(&cfg.AllColumns),
		huh.NewInput().Title("Proxy file").Value(&cfg.ProxyFile),
		huh.NewConfirm().Title("Only scrape through proxies?").Description("Never connect to Google directly. A search stops if no proxy works").Value(&cfg.RequireProxy),
		huh.NewInput().Title("Country").Description("Two letter code results are localised for, like uk, us, ca").Value(&cfg.Region).Validate(required),
	).Title("General")

	filters := huh.NewGroup(
		huh.NewMultiSelect[string]().Title("Only keep businesses that have").Options(
			huh.NewOption("A website", "website"),
			huh.NewOption("A phone number", "phone"),
			huh.NewOption("An address", "address"),
		).Value(&mustHave),
		huh.NewInput().Title("Minimum rating").Description("0 to turn off").Value(&rating).Validate(numberBetween(0, 5)),
		huh.NewInput().Title("Minimum reviews").Description("0 to turn off").Value(&reviews).Validate(wholeNumberBetween(0, 1000000)),
	).Title("Filters")

	if form(general, filters) != nil {
		return
	}

	cfg.Filters = config.Filters{
		RequireWebsite: slices.Contains(mustHave, "website"),
		RequirePhone:   slices.Contains(mustHave, "phone"),
		RequireAddress: slices.Contains(mustHave, "address"),
	}
	cfg.Parallel, _ = strconv.Atoi(strings.TrimSpace(parallel))
	cfg.Filters.MinRating, _ = strconv.ParseFloat(strings.TrimSpace(rating), 64)
	cfg.Filters.MinReviews, _ = strconv.Atoi(strings.TrimSpace(reviews))
	cfg.Region = strings.ToLower(strings.TrimSpace(cfg.Region))
	cfg.ProxyFile = strings.Trim(strings.TrimSpace(cfg.ProxyFile), `"'`)

	a.cfg = cfg
	a.notice = good.Render("Settings saved")
	if err := cfg.Save(); err != nil {
		a.notice = bad.Render("couldn't save settings: " + err.Error())
	}
	if err := a.reload(); err != nil {
		a.notice = bad.Render(err.Error())
	}
}

func wholeNumberBetween(low, high int) func(string) error {
	return func(s string) error {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < low || n > high {
			return fmt.Errorf("enter a whole number from %d to %d", low, high)
		}
		return nil
	}
}

func numberBetween(low, high float64) func(string) error {
	return func(s string) error {
		n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil || n < low || n > high {
			return fmt.Errorf("enter a number from %g to %g", low, high)
		}
		return nil
	}
}
