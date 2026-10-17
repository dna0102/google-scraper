package gmaps

import "strings"

type Place struct {
	Query          string
	Name           string
	Address        string
	Website        string
	WebsiteURL     string
	Phone          string
	PhoneIntl      string
	ReviewsCount   int
	ReviewsAverage float64
	StoreShopping  bool
	InStorePickup  bool
	StoreDelivery  bool
	PlaceType      string
	Categories     []string
	OpensAt        string
	Introduction   string
	PlaceID        string
	DataID         string
	Latitude       float64
	Longitude      float64
}

func (p Place) MapsURL() string {
	if p.PlaceID == "" {
		return ""
	}
	return "https://www.google.com/maps/place/?q=place_id:" + p.PlaceID
}

func (p Place) key() string {
	if p.DataID != "" {
		return p.DataID
	}
	return strings.ToLower(strings.TrimSpace(p.Name)) + "|" + strings.ToLower(strings.TrimSpace(p.Address))
}

func (p *Place) merge(details Place) {
	fill(&p.Introduction, details.Introduction)
	fill(&p.Address, details.Address)
	fill(&p.Website, details.Website)
	fill(&p.WebsiteURL, details.WebsiteURL)
	fill(&p.Phone, details.Phone)
	fill(&p.PhoneIntl, details.PhoneIntl)
	fill(&p.OpensAt, details.OpensAt)
	p.StoreShopping = p.StoreShopping || details.StoreShopping
	p.InStorePickup = p.InStorePickup || details.InStorePickup
	p.StoreDelivery = p.StoreDelivery || details.StoreDelivery
}

func fill(dst *string, src string) {
	if *dst == "" {
		*dst = src
	}
}

func Dedupe(places []Place) (unique []Place, removed int) {
	seen := make(map[string]bool, len(places))
	for _, p := range places {
		if p.Name == "" || seen[p.key()] {
			removed++
			continue
		}
		seen[p.key()] = true
		unique = append(unique, p)
	}
	return unique, removed
}
