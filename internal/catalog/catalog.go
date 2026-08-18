// Package catalog embeds the generated price catalog (cache format). The
// file is regenerated with `make catalog` from testdata/catalog/main.tf;
// manual updates only. provider.LoadEmbedded turns it into the last-resort
// Pricer layer so credentials-free runs get real prices out of the box.
package catalog

import (
	_ "embed"
)

//go:embed prices.json
var Prices []byte

//go:embed date.txt
var Date string
