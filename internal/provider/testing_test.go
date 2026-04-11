// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http/httptest"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testProviderFactories returns a protov6 provider factory map wired to an
// in-memory EverflowProvider configured against the given httptest.Server.
// This is shared plumbing for every resource's unit tests — future
// affiliate and offer tests pull from here too.
//
// The provider is configured via HCL in each step rather than via env vars
// so tests remain hermetic even when EVERFLOW_API_KEY is set on the
// developer's shell.
func testProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"everflow": providerserver.NewProtocol6WithError(New("test")()),
	}
}

// testProviderConfig returns an HCL fragment that points the provider at
// the given test server URL. Every resource unit test prepends this to its
// configuration so plan/apply round-trips hit the fake Everflow, not the
// real one.
func testProviderConfig(srv *httptest.Server) string {
	return fmt.Sprintf(`
provider "everflow" {
  api_key  = "test-key"
  base_url = %q
}
`, srv.URL)
}
