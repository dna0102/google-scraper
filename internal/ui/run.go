package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"

	"gscraper/internal/config"
	"gscraper/internal/gmaps"
	"gscraper/internal/output"
)

type Job struct {
	Queries      []string
	Limit        int
	Details      bool
	Output       string
	Append       bool
	AllColumns   bool
	RequireProxy bool
	Filters      config.Filters
}

func Run(ctx context.Context, client *gmaps.Client, job Job) (saved int, err error) {
	start := time.Now()
	if client, err = connect(ctx, client, job.RequireProxy); err != nil {
		return 0, err
	}

	printIntro(job)
	places, failed := scrape(ctx, client, job)
	if ctx.Err() != nil {
		fmt.Println(warn.Render(fmt.Sprintf("\n  Stopped early, keeping the %d businesses found so far", len(places))))
	}

	found := len(places)
	places, dupes := gmaps.Dedupe(places)
	if job.Details && ctx.Err() == nil && len(places) > 0 {
		fetchDescriptions(ctx, client, places)
	}

	places, filtered := job.Filters.Apply(places)
	if len(places) == 0 {
		msg := "\n  Nothing to save."
		if filtered > 0 {
			msg += fmt.Sprintf(" Your filters removed all %d businesses.", filtered)
		}
		fmt.Println(warn.Render(msg))
		return 0, nil
	}

	if err := output.Write(job.Output, places, job.AllColumns, job.Append); err != nil {
		return 0, err
	}
	fmt.Println()
	fmt.Println(summary(job, places, found, dupes, filtered, failed, time.Since(start)))
	return len(places), nil
}

func connect(ctx context.Context, client *gmaps.Client, require bool) (*gmaps.Client, error) {
	if client.ProxyCount() == 0 {
		if require {
			return nil, errors.New(`proxies are required but none are loaded. Add some to your proxy file or turn off "Only scrape through proxies"`)
		}
		fmt.Printf("  %s\n\n", warn.Render("No proxies loaded, connecting directly"))
		return client, nil
	}

	fmt.Printf("  Checking %d %s…", client.ProxyCount(), plural(client.ProxyCount(), "proxy", "proxies"))
	working, checks := client.Verify(ctx)
	if tty {
		fmt.Print("\r\033[K")
	} else {
		fmt.Println()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var ips []string
	shown := 0
	for _, c := range checks {
		if c.Err != nil && shown < 5 {
			shown++
			fmt.Printf("  %s %s  %s\n", bad.Render("✗"), gmaps.Redact(c.Proxy), faint.Render(shortErr(c.Err)))
		}
		if c.Err == nil && c.IP != "" && !slices.Contains(ips, c.IP) {
			ips = append(ips, c.IP)
		}
	}
	if dead := len(checks) - working.ProxyCount(); dead > shown {
		fmt.Println(faint.Render(fmt.Sprintf("  …and %d more not working", dead-shown)))
	}
	if working.ProxyCount() == 0 {
		return nil, errors.New("none of your proxies are working, stopped before contacting Google")
	}

	line := fmt.Sprintf("✓ Using %d of %d %s", working.ProxyCount(), client.ProxyCount(), plural(client.ProxyCount(), "proxy", "proxies"))
	if len(ips) > 0 {
		line += " · exit IP " + strings.Join(ips[:min(3, len(ips))], ", ")
	}
	if len(ips) > 3 {
		line += fmt.Sprintf(" +%d more", len(ips)-3)
	}
	fmt.Printf("  %s\n\n", good.Render(line))
	return working, nil
}

func printIntro(job Job) {
	limit := "all results"
	if job.Limit > 0 {
		limit = fmt.Sprintf("%d results", job.Limit)
	}
	count := fmt.Sprintf("%d %s", len(job.Queries), plural(len(job.Queries), "query", "queries"))
	fmt.Printf("  %s %s\n\n", bold.Render(count), faint.Render("· "+limit+" each · Ctrl+C to stop early and keep what's found"))
}

func scrape(ctx context.Context, client *gmaps.Client, job Job) (places []gmaps.Place, failed int) {
	total := len(job.Queries)
	width := 0
	for _, q := range job.Queries {
		width = min(max(width, len([]rune(q))), 44)
	}

	results := make([][]gmaps.Place, total)
	bar := newProgress(total, plural(total, "query", "queries"))
	jobs := make(chan int)
	var wg sync.WaitGroup

	for range min(total, client.Parallel()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				started := time.Now()
				found, err := client.Search(ctx, job.Queries[i], job.Limit)
				name := fmt.Sprintf("%-*s", width, truncate(job.Queries[i], width))

				bar.Lock()
				results[i] = found
				bar.done++
				switch {
				case err == nil:
					bar.println(fmt.Sprintf("  %s %s  %s", good.Render("✓"), name, faint.Render(fmt.Sprintf("%4d found  %4.1fs", len(found), time.Since(started).Seconds()))))
				case ctx.Err() == nil:
					failed++
					bar.println(fmt.Sprintf("  %s %s  %s", bad.Render("✗"), name, bad.Render(err.Error())))
				}
				bar.Unlock()
			}
		}()
	}

feed:
	for i := range total {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	bar.finish()

	for _, r := range results {
		places = append(places, r...)
	}
	return places, failed
}

func fetchDescriptions(ctx context.Context, client *gmaps.Client, places []gmaps.Place) {
	fmt.Println()
	bar := newProgress(len(places), "descriptions")
	missed := client.Enrich(ctx, places, func(done int) {
		bar.done = done
		bar.draw()
	})
	bar.finish()
	if missed > 0 {
		fmt.Println(warn.Render(fmt.Sprintf("  %d descriptions couldn't be fetched", missed)))
	}
}

func summary(job Job, places []gmaps.Place, found, dupes, filtered, failed int, took time.Duration) string {
	var withWebsite, withPhone, rated int
	var ratingSum float64
	for _, p := range places {
		if p.Website != "" {
			withWebsite++
		}
		if p.Phone != "" {
			withPhone++
		}
		if p.ReviewsAverage > 0 {
			rated++
			ratingSum += p.ReviewsAverage
		}
	}
	share := func(n int) string {
		return fmt.Sprintf("%d  %s", n, faint.Render(fmt.Sprintf("(%d%%)", n*100/len(places))))
	}

	rows := [][2]string{{"Found", fmt.Sprint(found)}}
	if dupes > 0 {
		rows = append(rows, [2]string{"Duplicates", fmt.Sprint(-dupes)})
	}
	if filtered > 0 {
		rows = append(rows, [2]string{"Filtered", fmt.Sprintf("%d  %s", -filtered, faint.Render(strings.Join(job.Filters.Describe(), ", ")))})
	}
	if failed > 0 {
		rows = append(rows, [2]string{"Failed", bad.Render(fmt.Sprint(failed))})
	}
	rows = append(rows, [2]string{"With website", share(withWebsite)}, [2]string{"With phone", share(withPhone)})
	if rated > 0 {
		rows = append(rows, [2]string{"Avg rating", fmt.Sprintf("%.1f★", ratingSum/float64(rated))})
	}
	rows = append(rows, [2]string{"Time", fmt.Sprintf("%.1fs", took.Seconds())})

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", good.Bold(true).Render(fmt.Sprintf("✓ Saved %d businesses", len(places))))
	for _, r := range rows {
		fmt.Fprintf(&b, "%-14s%s\n", faint.Render(r[0]), r[1])
	}
	fmt.Fprintf(&b, "\n%s", bold.Render(job.Output))
	return lipgloss.NewStyle().MarginLeft(2).Render(box.Render(b.String()))
}
