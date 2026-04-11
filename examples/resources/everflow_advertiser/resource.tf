resource "everflow_advertiser" "example" {
  name                  = "Example Advertiser"
  account_status        = "active"
  network_employee_id   = 1
  default_currency_id   = "USD"
  reporting_timezone_id = 80 # America/New_York

  internal_notes = "Managed by Terraform"
}
