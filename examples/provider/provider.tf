terraform {
  required_providers {
    everflow = {
      source  = "BorrowBetter/everflow"
      version = "~> 0.1"
    }
  }
}

# Authenticate via the `api_key` attribute or the EVERFLOW_API_KEY
# environment variable. The base_url attribute is optional and only
# useful for test overrides; production deployments should leave it unset.
provider "everflow" {
  api_key = var.everflow_api_key
}

variable "everflow_api_key" {
  description = "Everflow Network API key (X-Eflow-Api-Key)."
  type        = string
  sensitive   = true
}
