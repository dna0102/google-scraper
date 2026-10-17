package gmaps

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

func at(v any, path ...int) any {
	for _, i := range path {
		arr, ok := v.([]any)
		if !ok || i < 0 || i >= len(arr) {
			return nil
		}
		v = arr[i]
	}
	return v
}

func str(v any, path ...int) string  { s, _ := at(v, path...).(string); return s }
func num(v any, path ...int) float64 { f, _ := at(v, path...).(float64); return f }
func list(v any, path ...int) []any  { a, _ := at(v, path...).([]any); return a }

func decode(body []byte) ([]any, error) {
	var root []any
	err := json.Unmarshal(bytes.TrimPrefix(body, []byte(xssi)), &root)
	return root, err
}

func parseSearch(body []byte) ([]Place, int, error) {
	root, err := decode(body)
	if err != nil {
		return nil, 0, err
	}
	var places []Place
	results := list(root, 64)
	for _, r := range results {
		if b := list(r, 1); isPlace(b) {
			places = append(places, parsePlace(b))
		}
	}
	if len(results) == 0 {
		for _, b := range findPlaces(root, nil) {
			places = append(places, parsePlace(b))
		}
		return places, len(places), nil
	}
	return places, len(results), nil
}

func parseDetails(body []byte) (Place, error) {
	root, err := decode(body)
	if err != nil {
		return Place{}, err
	}
	if b := list(root, 6); isPlace(b) {
		return parsePlace(b), nil
	}
	return Place{}, errors.New("no place in response")
}

func isPlace(b []any) bool {
	return len(b) > 100 && strings.HasPrefix(str(b, 10), "0x") && str(b, 11) != ""
}

func findPlaces(v any, out [][]any) [][]any {
	arr, ok := v.([]any)
	if !ok {
		return out
	}
	if isPlace(arr) {
		return append(out, arr)
	}
	for _, child := range arr {
		out = findPlaces(child, out)
	}
	return out
}

func parsePlace(b []any) Place {
	p := Place{
		Name:           str(b, 11),
		DataID:         str(b, 10),
		PlaceID:        str(b, 78),
		Latitude:       num(b, 9, 2),
		Longitude:      num(b, 9, 3),
		ReviewsAverage: num(b, 4, 7),
		ReviewsCount:   int(num(b, 4, 8)),
		Website:        str(b, 7, 1),
		WebsiteURL:     unwrap(str(b, 7, 0)),
		Phone:          str(b, 178, 0, 3),
		PhoneIntl:      str(b, 178, 0, 1, 1, 0),
		Address:        str(b, 39),
		OpensAt:        openStatus(b),
		Introduction:   description(b),
	}

	if p.Website == "" && p.WebsiteURL != "" {
		if u, err := url.Parse(p.WebsiteURL); err == nil {
			p.Website = strings.TrimPrefix(u.Hostname(), "www.")
		}
	}
	if p.Phone == "" {
		p.Phone = strings.NewReplacer(" ", "", "-", "").Replace(str(b, 178, 0, 0))
	}
	if p.Address == "" {
		p.Address = strings.TrimPrefix(str(b, 18), p.Name+", ")
	}
	if p.Address == p.Name {
		p.Address = ""
	}
	for _, c := range list(b, 13) {
		if s, ok := c.(string); ok {
			p.Categories = append(p.Categories, s)
		}
	}
	if len(p.Categories) > 0 {
		p.PlaceType = p.Categories[0]
	}
	serviceOptions(b, &p)
	return p
}

func openStatus(b []any) string {
	s := str(b, 203, 1, 4, 0)
	if s == "" {
		s = str(b, 34, 4, 4)
	}
	s = strings.ReplaceAll(s, "\u202f", "")
	if i := strings.LastIndex(s, "·"); i >= 0 {
		s = s[i+len("·"):]
	}
	return strings.TrimSpace(s)
}

func description(b []any) string {
	if s := str(b, 32, 1, 1); s != "" {
		return s
	}
	return strings.TrimSpace(str(b, 154, 0, 0))
}

func serviceOptions(b []any, p *Place) {
	var signals []string
	for _, section := range list(b, 100, 1) {
		for _, attr := range list(section, 2) {
			if num(attr, 2, 0) == 1 {
				signals = append(signals, str(attr, 0)+" "+str(attr, 1))
			}
		}
	}
	for _, hours := range list(b, 118) {
		signals = append(signals, str(hours, 0))
	}
	for _, s := range signals {
		s = strings.ToLower(s)
		p.StoreShopping = p.StoreShopping || containsAny(s, "in_store_shopping", "in-store shopping")
		p.InStorePickup = p.InStorePickup || containsAny(s, "pickup", "pick-up", "takeaway", "takeout", "kerbside", "curbside", "click and collect")
		p.StoreDelivery = p.StoreDelivery || strings.Contains(s, "delivery")
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func unwrap(u string) string {
	if !strings.HasPrefix(u, "/url?") {
		return u
	}
	if parsed, err := url.Parse(u); err == nil && parsed.Query().Get("q") != "" {
		return parsed.Query().Get("q")
	}
	return u
}
