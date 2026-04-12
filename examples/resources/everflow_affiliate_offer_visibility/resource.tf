# Grant an affiliate visibility on a private offer.
resource "everflow_affiliate_offer_visibility" "lpb_personal_loans" {
  network_affiliate_id = everflow_affiliate.lpb.network_affiliate_id
  network_offer_id     = everflow_offer.personal_loans.network_offer_id
}
