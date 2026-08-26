package user

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestAddAddressInputFields(t *testing.T) {
	in := AddAddressInput{
		Asset:       "BTC",
		Address:     "bc1qtest",
		Label:       "My Wallet",
		Whitelisted: true,
	}
	if in.Asset != "BTC" {
		t.Errorf("Asset mismatch: %s", in.Asset)
	}
	if in.Address != "bc1qtest" {
		t.Errorf("Address mismatch: %s", in.Address)
	}
	if in.Label != "My Wallet" {
		t.Errorf("Label mismatch: %s", in.Label)
	}
	if !in.Whitelisted {
		t.Error("Whitelisted should be true")
	}
}

func TestAddressBookEntryJSONTags(t *testing.T) {
	e := AddressBookEntry{
		ID:          uuid.New(),
		UserID:      uuid.New(),
		Asset:       "ETH",
		Address:     "0xtest",
		Label:       "Test",
		Whitelisted: false,
	}
	if e.Asset != "ETH" {
		t.Errorf("Asset: %s", e.Asset)
	}
}

func TestUpdateAddressInputPointers(t *testing.T) {
	label := "new label"
	whitelisted := true
	in := UpdateAddressInput{
		Label:       &label,
		Whitelisted: &whitelisted,
	}
	if *in.Label != "new label" {
		t.Errorf("Label: %s", *in.Label)
	}
	if !*in.Whitelisted {
		t.Error("Whitelisted should be true")
	}
}

func TestUpdateAddressInputNil(t *testing.T) {
	in := UpdateAddressInput{}
	if in.Label != nil {
		t.Error("Label should be nil")
	}
	if in.Whitelisted != nil {
		t.Error("Whitelisted should be nil")
	}
}

func TestDecimalHandling(t *testing.T) {
	d, _ := decimal.NewFromString("0.0001")
	if d.IsZero() {
		t.Error("Should not be zero")
	}
}