# Set a global postback URL that fires on all offer conversions
# for this affiliate.
resource "everflow_affiliate_postback" "global_conversion" {
  network_affiliate_id = everflow_affiliate.lpb.network_affiliate_id
  postback_url         = "https://lpb.example.com/postback?tid={transaction_id}&aid={affiliate_id}"
  pixel_type           = "conversion"
  pixel_status         = "active"
  description          = "Managed by Terraform"
}
