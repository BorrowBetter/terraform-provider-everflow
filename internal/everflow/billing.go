// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package everflow

// BillingDetails holds the inner "details" block of a billing object.
type BillingDetails struct {
	DayOfMonth int64 `json:"day_of_month"`
}

// Billing is the billing block Everflow requires on both affiliate and
// advertiser creation (and PUT updates). Billing is write-only: the API
// accepts it on POST/PUT but does NOT return it on GET. Consequently it
// is not exposed in the Terraform schema — the provider hardcodes
// sensible defaults matching the Everflow UI (monthly, no payment,
// day 1).
//
// The Everflow API requires an inner "billing" key (typically an empty
// object) and a "tax_id" field inside the billing block. Omitting either
// causes a 400 "Invalid parameters" on affiliate PUT/POST (#22).
type Billing struct {
	Inner            map[string]any `json:"billing"`
	BillingFrequency string         `json:"billing_frequency"`
	PaymentType      string         `json:"payment_type"`
	TaxID            string         `json:"tax_id"`
	Details          BillingDetails `json:"details"`
}

// DefaultBilling returns the billing block the provider sends on POST
// and injects into PUT bodies. Values match Everflow's UI defaults:
// monthly frequency, no payment method, day-of-month 1, empty inner
// billing object, and blank tax ID.
func DefaultBilling() Billing {
	return Billing{
		Inner:            map[string]any{},
		BillingFrequency: "monthly",
		PaymentType:      "none",
		TaxID:            "",
		Details:          BillingDetails{DayOfMonth: 1},
	}
}
