# terraform-provider-everflow

Terraform provider for [Everflow](https://everflow.io/), the affiliate
marketing platform. Lets you manage Everflow Network resources —
advertisers, offers, affiliates — as first-class Terraform resources
alongside the rest of your infrastructure.

> **Status:** pre-release. v0.1.0 ships the three core Network resources
> (advertiser, offer, affiliate) and is intended for BorrowBetter's own
> infrastructure, but the provider itself is vendor-agnostic and safe
> to reuse.

## Usage

```hcl
terraform {
  required_providers {
    everflow = {
      source  = "BorrowBetter/everflow"
      version = "~> 0.1"
    }
  }
}

provider "everflow" {
  api_key = var.everflow_api_key
}
```

The `api_key` attribute is required and sensitive. You can also set it
via the `EVERFLOW_API_KEY` environment variable if you prefer to keep
it out of your configuration.

An optional `base_url` attribute (default: `https://api.eflow.team`) is
available for test environments. Leave it unset in production.

## Development

### Prerequisites

- Go 1.25+ (see `go.mod` for the exact minimum)
- Terraform 1.5+ for manual testing against the compiled binary
- `make`

### Build and install locally

```sh
make install
```

This builds the provider binary and drops it into
`~/.terraform.d/plugins/registry.terraform.io/BorrowBetter/everflow/0.0.1-dev/<os>_<arch>/`
so Terraform's `dev_overrides` can find it.

### Point Terraform at the local binary

Add the following to `~/.terraformrc` (one-time setup):

```hcl
provider_installation {
  dev_overrides {
    "BorrowBetter/everflow" = "/absolute/path/to/terraform-provider-everflow"
  }
  direct {}
}
```

The path should be the directory containing the compiled binary — i.e.
the `INSTALL_DIR` from `GNUmakefile`, or simply the repo root if you
prefer to `make build` without installing.

Once `dev_overrides` is in place, `terraform plan` in any consumer
project will use the local binary; Terraform will emit a warning about
the override being active, which is expected.

### Tests

```sh
make test        # unit tests (fast, no network)
make testacc     # acceptance tests (requires TF_ACC=1 and a real API key)
```

Unit tests use `httptest` round-trips and run in under a second.
Acceptance tests hit the real Everflow API and require
`EVERFLOW_API_KEY` in the environment.

### Docs

```sh
make docs
```

Regenerates `docs/` from the resource schemas and the HCL in
`examples/`. The Registry reads `docs/*.md` when it publishes a
release, so keep this in sync with schema changes.

### Lint

```sh
make lint
```

Runs `go vet` and a `gofmt -l` check that fails if any file needs
reformatting.

## Releasing

Releases are fully automated by GitHub Actions on tag push. A signed,
multi-platform release is produced by `goreleaser`, and the Terraform
Registry picks it up automatically on every subsequent tag (after the
initial one-time manual publish click).

```sh
git tag -s v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The release workflow pulls the GPG signing key from AWS Secrets Manager
(`borrowbetter/terraform/provider-gpg`) via GitHub OIDC — no static AWS
credentials and no GPG key material in GitHub Secrets. See
`.github/workflows/release.yml` for the exact flow.

## Scope

v0.1.0 ships:

- `everflow_advertiser` — `/v1/networks/advertisers`
- `everflow_offer` — `/v1/networks/offers`
- `everflow_affiliate` — `/v1/networks/affiliates`

Explicitly out of scope for v0.1.0 (planned for future releases):
traffic sources, employees, currencies, timezones, campaigns, tracking
domains, creatives.

## License

[Mozilla Public License 2.0](./LICENSE). Matches the convention for
Terraform providers published to the public Registry.
