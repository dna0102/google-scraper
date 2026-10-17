package gmaps

import (
	"context"
	"fmt"
	"sync"
)

const (
	pageSize      = 20
	maxOffset     = 1000
	pagesPerRound = 10
)

type page struct {
	places  []Place
	entries int
	err     error
}

func (c *Client) Search(ctx context.Context, query string, limit int) ([]Place, error) {
	var found []Place
	seen := map[string]bool{}

	for offset := 0; offset < maxOffset; {
		count := min(c.cfg.Parallel, pagesPerRound)
		if limit > 0 {
			count = min(count, max(1, (limit-len(found)+pageSize-1)/pageSize))
		}

		for i, pg := range c.fetchPages(ctx, query, offset, count) {
			if pg.err != nil {
				return trim(found, limit), fmt.Errorf("page %d: %w", offset/pageSize+i+1, pg.err)
			}
			for _, p := range pg.places {
				if !seen[p.key()] {
					seen[p.key()] = true
					p.Query = query
					found = append(found, p)
				}
			}
			if pg.entries < pageSize {
				return trim(found, limit), nil
			}
		}

		if limit > 0 && len(found) >= limit {
			break
		}
		offset += count * pageSize
	}
	return trim(found, limit), nil
}

func (c *Client) fetchPages(ctx context.Context, query string, offset, count int) []page {
	pages := make([]page, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := c.get(ctx, c.searchURL(query, offset+i*pageSize, pageSize))
			if err != nil {
				pages[i].err = err
				return
			}
			pages[i].places, pages[i].entries, pages[i].err = parseSearch(body)
		}()
	}
	wg.Wait()
	return pages
}

func trim(places []Place, limit int) []Place {
	if limit > 0 && len(places) > limit {
		return places[:limit]
	}
	return places
}

func (c *Client) Details(ctx context.Context, id string) (Place, error) {
	body, err := c.get(ctx, c.detailsURL(id))
	if err != nil {
		return Place{}, err
	}
	return parseDetails(body)
}

func (c *Client) Enrich(ctx context.Context, places []Place, progress func(done int)) (failed int) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	done := 0

	for i := range places {
		if places[i].DataID == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			details, err := c.Details(ctx, places[i].DataID)

			mu.Lock()
			defer mu.Unlock()
			done++
			if err != nil {
				failed++
			} else {
				places[i].merge(details)
			}
			if progress != nil {
				progress(done)
			}
		}()
	}
	wg.Wait()
	return failed
}
