resource "everflow_affiliate" "example" {
  name                = "Example Affiliate"
  account_status      = "active"
  network_employee_id = 1
  default_currency_id = "USD"

  billing = {
    billing_frequency = "monthly"
    payment_type      = "none"
    day_of_month      = 1
  }

  internal_notes = "Managed by Terraform"
}
