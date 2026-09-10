package catalog

import "errors"

var ErrInvalidOfferPrice = errors.New("offer price must be positive and lower than regular price")

// CatalogPrice is the server-authoritative regular/offer pair for a purchasable item.
// All amounts are integer KWD fils.
type CatalogPrice struct {
	RegularMinorUnits   int64  `json:"regular_minor_units"`
	OfferMinorUnits     *int64 `json:"offer_minor_units,omitempty"`
	EffectiveMinorUnits int64  `json:"effective_minor_units"`
	Currency            string `json:"currency"`
}

func NewCatalogPrice(regular int64, offer *int64) (CatalogPrice, error) {
	if regular < 0 {
		return CatalogPrice{}, ErrInvalidPrice
	}
	if offer != nil && (*offer <= 0 || *offer >= regular) {
		return CatalogPrice{}, ErrInvalidOfferPrice
	}
	effective := regular
	if offer != nil {
		effective = *offer
	}
	return CatalogPrice{
		RegularMinorUnits:   regular,
		OfferMinorUnits:     offer,
		EffectiveMinorUnits: effective,
		Currency:            "KWD",
	}, nil
}
