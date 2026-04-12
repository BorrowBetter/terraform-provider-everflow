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
type Billing struct {
	BillingFrequency string         `json:"billing_frequency"`
	PaymentType      string         `json:"payment_type"`
	Details          BillingDetails `json:"details"`
}

// DefaultBilling returns the billing block the provider sends on POST
// and injects into PUT bodies. Values match Everflow's UI defaults:
// monthly frequency, no payment method, day-of-month 1.
func DefaultBilling() Billing {
	return Billing{
		BillingFrequency: "monthly",
		PaymentType:      "none",
		Details:          BillingDetails{DayOfMonth: 1},
	}
}
