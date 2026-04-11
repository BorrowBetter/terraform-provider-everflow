// Copyright (c) BorrowBetter
// SPDX-License-Identifier: MPL-2.0

package provider

import "github.com/hashicorp/terraform-plugin-framework/path"

// schemaPath is a tiny helper to keep the provider code terse when reporting
// attribute-scoped diagnostics.
func schemaPath(name string) path.Path {
	return path.Root(name)
}
