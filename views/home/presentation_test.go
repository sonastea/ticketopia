package home

import (
	"testing"

	"github.com/sonastea/ticketopia/internal/models"
)

func TestPriceLabel(t *testing.T) {
	min, max := "20", "40"
	for _, tt := range []struct {
		name   string
		prices []models.PriceRange
		want   string
	}{
		{"range", []models.PriceRange{{Currency: "USD", Min: &min, Max: &max}}, "USD 20 - 40"},
		{"equal bounds", []models.PriceRange{{Currency: "USD", Min: &min, Max: &min}}, "USD 20"},
		{"minimum only", []models.PriceRange{{Currency: "USD", Min: &min}}, "USD 20"},
		{"unknown", nil, "Price not listed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := priceLabel(tt.prices); got != tt.want {
				t.Fatalf("priceLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}
