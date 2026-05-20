package articles_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/shophub-project-2026/shop/internal/articles"
)

func TestCreateInput_Validate(t *testing.T) {
	cases := []struct {
		name    string
		in      articles.CreateInput
		wantErr bool
	}{
		{"ok", articles.CreateInput{Name: "Widget", Quantity: 1, Price: 10}, false},
		{"trim", articles.CreateInput{Name: "  Widget  ", Quantity: 0, Price: 1}, false},
		{"empty name", articles.CreateInput{Name: "", Quantity: 1, Price: 1}, true},
		{"name too long", articles.CreateInput{Name: strings.Repeat("x", articles.MaxNameLength+1), Quantity: 1, Price: 1}, true},
		{"negative qty", articles.CreateInput{Name: "x", Quantity: -1, Price: 1}, true},
		{"zero price", articles.CreateInput{Name: "x", Quantity: 1, Price: 0}, true},
		{"absurd price", articles.CreateInput{Name: "x", Quantity: 1, Price: articles.MaxPrice + 1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, articles.ErrInvalidInput) {
				t.Errorf("error not wrapped as ErrInvalidInput: %v", err)
			}
		})
	}
}

func TestUpdateInput_ValidatePartial(t *testing.T) {
	q := 5
	p := 9.99
	in := articles.UpdateInput{Name: "x", Quantity: &q, Price: &p}
	if err := in.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}

	bad := -1
	in2 := articles.UpdateInput{Quantity: &bad}
	if err := in2.Validate(); err == nil {
		t.Error("expected error for negative quantity")
	}
}
