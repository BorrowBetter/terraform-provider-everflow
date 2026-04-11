resource "everflow_affiliate" "example" {
  name                = "Example Affiliate"
  account_status      = "active"
  network_employee_id = 1
  default_currency_id = "USD"

  internal_notes = "Managed by Terraform"
}
