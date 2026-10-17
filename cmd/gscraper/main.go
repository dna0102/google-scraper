package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"gscraper/internal/config"
	"gscraper/internal/gmaps"
	"gscraper/internal/ui"
)

const usage = `GScraper - Google Maps lead scraper

Run without arguments for the interactive menu, or:

  gscraper -s "plumbers in Leicester" -t 50
  gscraper -b queries/towns.txt -t 0 -o results/towns.xlsx
  gscraper -s "dentists in Leeds" -proxies proxies.txt -c 24

Flags:
`

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}

	query := flag.String("s", "", "search query")
	batch := flag.String("b", "", "text file with one query per line")
	limit := flag.Int("t", 20, "results per query, 0 for all")
	out := flag.String("o", "", "output file, .csv .xlsx or .json (default results/<query>_<time>.csv)")
	details := flag.Bool("details", false, "include business descriptions")
	allColumns := flag.Bool("all", cfg.AllColumns, "save every column instead of the standard set")
	appendCSV := flag.Bool("append", false, "append to an existing csv")
	proxy := flag.String("proxy", "", "single proxy, any supported format")
	proxyFile := flag.String("proxies", cfg.ProxyFile, "proxy list, one per line")
	requireProxy := flag.Bool("require-proxy", cfg.RequireProxy, "never connect directly, stop if no proxy works")
	parallel := flag.Int("c", cfg.Parallel, "parallel requests")
	noFilters := flag.Bool("no-filters", false, "ignore the filters saved in config.json")
	region := flag.String("gl", cfg.Region, "country results are localised for")

	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *query == "" && *batch == "" {
		if err := ui.Start(); err != nil {
			fail(err)
		}
		return
	}

	proxies, err := loadProxies(*proxy, *proxyFile)
	if err != nil {
		fail(err)
	}

	client, err := gmaps.NewClient(gmaps.Config{Language: cfg.Language, Region: *region, Proxies: proxies, Parallel: *parallel, Retries: 3, Timeout: 20 * time.Second})
	if err != nil {
		fail(err)
	}

	job := ui.Job{Queries: []string{*query}, Limit: max(*limit, 0), Details: *details, Output: *out, Append: *appendCSV, AllColumns: *allColumns, RequireProxy: *requireProxy}
	name := *query
	if *batch != "" {
		if job.Queries, err = ui.ReadQueries(*batch); err != nil {
			fail(err)
		}
		if len(job.Queries) == 0 {
			fail(fmt.Errorf("%s has no queries", *batch))
		}
		name = ui.BatchName(*batch)
	}
	if job.Output == "" {
		job.Output = ui.OutputPath(name, "csv")
	}
	if !*noFilters {
		job.Filters = cfg.Filters
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if _, err := ui.Run(ctx, client, job); err != nil {
		fail(err)
	}
}

func loadProxies(single, file string) ([]string, error) {
	if single != "" {
		proxy, err := gmaps.ParseProxy(single)
		if err != nil {
			return nil, fmt.Errorf("proxy: %w", err)
		}
		return []string{proxy}, nil
	}
	if file == "" {
		return nil, nil
	}

	proxies, skipped, err := gmaps.LoadProxies(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, line := range skipped {
		fmt.Fprintln(os.Stderr, "skipped proxy", line)
	}
	return proxies, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
