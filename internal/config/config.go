package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"gscraper/internal/gmaps"
)

const Path = "config.json"

type Filters struct {
	RequireWebsite bool    `json:"require_website"`
	RequirePhone   bool    `json:"require_phone"`
	RequireAddress bool    `json:"require_address"`
	MinRating      float64 `json:"min_rating"`
	MinReviews     int     `json:"min_reviews"`
}

type Config struct {
	Parallel     int     `json:"parallel"`
	ProxyFile    string  `json:"proxy_file"`
	RequireProxy bool    `json:"require_proxy"`
	AllColumns   bool    `json:"all_columns"`
	Language     string  `json:"language"`
	Region       string  `json:"region"`
	Filters      Filters `json:"filters"`
}

func Default() Config {
	return Config{Parallel: 8, ProxyFile: "proxies.txt", Language: "en", Region: "uk"}
}

func Load() (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(Path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("%s: %w", Path, err)
	}
	cfg.Parallel = max(cfg.Parallel, 1)
	return cfg, nil
}

func (c Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path, append(data, '\n'), 0o644)
}

func (f Filters) Pass(p gmaps.Place) bool {
	switch {
	case f.RequireWebsite && p.Website == "":
		return false
	case f.RequirePhone && p.Phone == "":
		return false
	case f.RequireAddress && p.Address == "":
		return false
	case f.MinRating > 0 && p.ReviewsAverage < f.MinRating:
		return false
	case f.MinReviews > 0 && p.ReviewsCount < f.MinReviews:
		return false
	}
	return true
}

func (f Filters) Apply(places []gmaps.Place) (kept []gmaps.Place, removed int) {
	for _, p := range places {
		if f.Pass(p) {
			kept = append(kept, p)
		} else {
			removed++
		}
	}
	return kept, removed
}

func (f Filters) Describe() []string {
	var rules []string
	if f.RequireWebsite {
		rules = append(rules, "website")
	}
	if f.RequirePhone {
		rules = append(rules, "phone")
	}
	if f.RequireAddress {
		rules = append(rules, "address")
	}
	if f.MinRating > 0 {
		rules = append(rules, fmt.Sprintf("%.1f★+", f.MinRating))
	}
	if f.MinReviews > 0 {
		rules = append(rules, fmt.Sprintf("%d+ reviews", f.MinReviews))
	}
	return rules
}
