resource "everflow_advertiser" "parent" {
  name                  = "Example Advertiser"
  account_status        = "active"
  network_employee_id   = 1
  default_currency_id   = "USD"
  reporting_timezone_id = 80
}

resource "everflow_offer" "example" {
  name                       = "Example Offer"
  network_advertiser_id      = everflow_advertiser.parent.network_advertiser_id
  destination_url            = "https://example.com/landing"
  offer_status               = "active"
  currency_id                = "USD"
  conversion_method          = "server_postback"
  network_tracking_domain_id = 1

  internal_notes = "Managed by Terraform"

  payout_revenue {
    entry_name     = "Base"
    payout_type    = "cpa"
    payout_amount  = 5.00
    revenue_type   = "rpa"
    revenue_amount = 10.00
    is_default     = true
    is_private     = false
  }
}
