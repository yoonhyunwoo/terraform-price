# AGENTS.md

Guidance for future agent instances — conventions and rationale only. For structure,
dependencies, and commands, read the code directly (it is small).

## Do not rename package `price` to `pricing`

The AWS adapter lives in `internal/price` (package `price`). `price.go` imports
`github.com/aws/aws-sdk-go-v2/service/pricing`, whose own package name is also `pricing`.
Naming the local package `pricing` collides with that import and forces an alias onto every
import line. The intended response to the "inconsistent with AWS naming" smell is to leave the
package as `price` — not to alias the AWS import everywhere.

## Price cache is shared across AWS profiles — keep the profile out of the cache key

`internal/price/cached.go` keys cached results by
`serviceCode | location(region) filter | spec filters | preferUnit` and intentionally omits the
AWS profile. AWS OnDemand list prices are public and depend only on region + resource spec,
never on the querying account — discounts (RI / Savings Plan / EDP) apply later at billing, not
at the Price List API (see the caveat printed in `output.go`). One cache file therefore correctly
serves every profile; adding the profile to the key would be a wrong "fix" that duplicates
identical prices. TTL is 7 days (`price.CacheTTL`); `--no-cache` bypasses the cache for a run,
and deleting `$UserCacheDir/terraform-price/prices.json` forces a full refresh.

## Var / locals resolution is limited to two hardcoded files

`resolver.NewResolver` parses only `terraform.tfvars` and `locals.tf` in the target directory.
It does **not** read `*.auto.tfvars`, `terraform.tfvars.json`, `-var` / `-var-file` flags, or
`TF_VAR_*` environment variables. `parser.ParseDir` is a separate path and reads every `*.tf`.
Symptom of this gap: a resource prints `unresolved: …` even though its value is
defined in a non-standard tfvars source. Extending resolution means teaching the resolver about
those sources explicitly — the parser already covers the `.tf` side.
