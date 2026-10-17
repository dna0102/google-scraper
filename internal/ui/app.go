package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"gscraper/internal/config"
	"gscraper/internal/gmaps"
)

type App struct {
	cfg     config.Config
	client  *gmaps.Client
	proxies []string
	skipped []string
	notice  string
}

func Start() error {
	cfg, err := config.Load()
	app := &App{cfg: cfg}
	if err != nil {
		app.notice = warn.Render("config.json couldn't be read, using defaults: " + err.Error())
	}
	if err := app.reload(); err != nil {
		return err
	}

	leave := enterScreen()
	defer leave()

	for {
		header(app.status())
		if app.notice != "" {
			fmt.Printf("  %s\n\n", app.notice)
			app.notice = ""
		}

		choice := "search"
		err := ask(huh.NewSelect[string]().Title("What would you like to do?").Options(
			huh.NewOption("Search", "search"),
			huh.NewOption("Batch search", "batch"),
			huh.NewOption("Browse results", "browse"),
			huh.NewOption("Proxies", "proxies"),
			huh.NewOption("Settings", "settings"),
			huh.NewOption("Quit", "quit"),
		).Value(&choice))
		if errors.Is(err, huh.ErrUserAborted) || choice == "quit" {
			return nil
		}
		if err != nil {
			return err
		}

		switch choice {
		case "search":
			app.search()
		case "batch":
			app.batch()
		case "browse":
			app.browse()
		case "proxies":
			app.proxyMenu()
		case "settings":
			app.settings()
		}
	}
}

func (a *App) reload() error {
	a.proxies, a.skipped = nil, nil
	if a.cfg.ProxyFile != "" {
		proxies, skipped, err := gmaps.LoadProxies(a.cfg.ProxyFile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			a.notice = warn.Render("couldn't read " + a.cfg.ProxyFile + ": " + err.Error())
		}
		a.proxies, a.skipped = proxies, skipped
	}

	client, err := gmaps.NewClient(gmaps.Config{Language: a.cfg.Language, Region: a.cfg.Region, Proxies: a.proxies, Parallel: a.cfg.Parallel, Retries: 3, Timeout: 20 * time.Second})
	if err != nil {
		return err
	}
	a.client = client
	return nil
}

func (a *App) status() string {
	proxies := "no proxies"
	if len(a.proxies) > 0 {
		proxies = fmt.Sprintf("%d %s", len(a.proxies), plural(len(a.proxies), "proxy", "proxies"))
	}
	if a.cfg.RequireProxy {
		proxies += " (required)"
	}

	filters := "no filters"
	if rules := a.cfg.Filters.Describe(); len(rules) > 0 {
		filters = "filters: " + strings.Join(rules, ", ")
	}

	columns := "standard columns"
	if a.cfg.AllColumns {
		columns = "all columns"
	}
	return fmt.Sprintf("%d parallel · %s · %s · %s", a.cfg.Parallel, proxies, filters, columns)
}

func resultsField(limit *int) *huh.Select[int] {
	return huh.NewSelect[int]().Title("How many results?").Options(
		huh.NewOption("20", 20),
		huh.NewOption("50", 50),
		huh.NewOption("100", 100),
		huh.NewOption("All of them (Google stops around 120-170)", 0),
	).Value(limit)
}

func formatField(format *string) *huh.Select[string] {
	return huh.NewSelect[string]().Title("Save as").Options(
		huh.NewOption("CSV", "csv"),
		huh.NewOption("Excel", "xlsx"),
		huh.NewOption("JSON", "json"),
	).Value(format)
}

func detailsField(details *bool) *huh.Confirm {
	return huh.NewConfirm().Title("Include business descriptions?").Description("One extra request per business, a bit slower").Value(details)
}

func (a *App) search() {
	header(a.status())
	var query string
	limit, format, details := 20, "csv", false

	err := form(huh.NewGroup(
		huh.NewInput().Title("What are you looking for?").Description("A business type and a place, like plumbers in Leicester").Value(&query).Validate(required),
		resultsField(&limit),
		formatField(&format),
		detailsField(&details),
	))
	if err != nil {
		return
	}
	a.execute(Job{Queries: []string{query}, Limit: limit, Details: details, Output: OutputPath(query, format)})
}

func (a *App) batch() {
	header(a.status())
	files, _ := filepath.Glob(filepath.Join("queries", "*.txt"))

	var sources []huh.Option[string]
	for _, f := range files {
		queries, _ := ReadQueries(f)
		label := fmt.Sprintf("%s  %s", filepath.Base(f), faint.Render(fmt.Sprintf("%d %s", len(queries), plural(len(queries), "query", "queries"))))
		sources = append(sources, huh.NewOption(label, f))
	}
	sources = append(sources, huh.NewOption("Paste a list", "paste"), huh.NewOption("Pick another file", "file"))

	source := sources[0].Value
	if ask(huh.NewSelect[string]().Title("Where are the queries?").Description("Put .txt files in the queries folder to see them here").Options(sources...).Value(&source)) != nil {
		return
	}

	var queries []string
	name := "batch"
	switch source {
	case "paste":
		var text string
		if ask(huh.NewText().Title("Paste your queries").Description("One per line").Lines(12).Value(&text).Validate(required)) != nil {
			return
		}
		queries = splitQueries(text)
	case "file":
		var path string
		if ask(huh.NewInput().Title("Path to a .txt file").Placeholder(`C:\leads\towns.txt`).Value(&path).Validate(fileExists)) != nil {
			return
		}
		queries, _ = ReadQueries(path)
		name = BatchName(path)
	default:
		queries, _ = ReadQueries(source)
		name = BatchName(source)
	}
	if len(queries) == 0 {
		a.notice = warn.Render("That list has no queries in it.")
		return
	}

	limit, format, details, start := 0, "csv", false, true
	err := form(huh.NewGroup(
		resultsField(&limit),
		formatField(&format),
		detailsField(&details),
		huh.NewConfirm().Title(fmt.Sprintf("Run %d %s?", len(queries), plural(len(queries), "query", "queries"))).Affirmative("Start").Negative("Cancel").Value(&start),
	))
	if err != nil || !start {
		return
	}
	a.execute(Job{Queries: queries, Limit: limit, Details: details, Output: OutputPath(name, format)})
}

func (a *App) execute(job Job) {
	header(a.status())
	job.Filters = a.cfg.Filters
	job.AllColumns = a.cfg.AllColumns
	job.RequireProxy = a.cfg.RequireProxy

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	saved, err := Run(ctx, a.client, job)
	stop()

	fmt.Println()
	switch {
	case err != nil:
		fmt.Println(bad.Render("  " + err.Error()))
		pause()
	case saved > 0:
		afterSave(job.Output)
	default:
		pause()
	}
}
