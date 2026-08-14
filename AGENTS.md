# AGENTS.md

Guidance for future agent instances — conventions and rationale only. For structure,
dependencies, and commands, read the code directly (it is small).

## Provider layout — neutral core, SDK only in the adapter

`internal/provider` holds the vendor-neutral types (`Filter`, `Query`, `Pricer`) and the
price cache; it must never import a cloud SDK. `internal/provider/awsprice` is the AWS
adapter — the only place allowed to import `aws-sdk-go-v2`, converting `provider.Filter`
to SDK filters at its edge. `internal/mapper` produces `provider.Query` values and is also
SDK-free. Boundary check: `go list -deps ./internal/mapper ./internal/provider` must not
contain `aws-sdk-go-v2`. This mirrors database/sql (driver owns conversion) and gocloud.dev
(provider package owns both directions); the k8s in-tree volume lesson (vendor types leaking
into a neutral API cost them a multi-year migration) is why the boundary is enforced early.
Never name the adapter package `aws` or `pricing` — both collide with SDK imports
(`aws-sdk-go-v2/aws`, `.../service/pricing`).

## Price cache is shared across AWS profiles — keep the profile out of the cache key

`internal/provider/cached.go` keys cached results by
`serviceCode | location(region) filter | spec filters | preferUnit` and intentionally omits the
AWS profile. AWS OnDemand list prices are public and depend only on region + resource spec,
never on the querying account — discounts (RI / Savings Plan / EDP) apply later at billing, not
at the Price List API (see the caveat printed in `output.go`). One cache file therefore correctly
serves every profile; adding the profile to the key would be a wrong "fix" that duplicates
identical prices. TTL is 7 days (`provider.CacheTTL`); `--no-cache` bypasses the cache for a run,
and deleting `$UserCacheDir/terraform-price/prices.json` forces a full refresh. The key string
format is load-bearing (existing cache files must keep hitting) — `TestCacheKeyFormatStable`
guards it.

## Var / locals resolution is limited to two hardcoded files

`resolver.NewResolver` parses only `terraform.tfvars` and `locals.tf` in the target directory.
It does **not** read `*.auto.tfvars`, `terraform.tfvars.json`, `-var` / `-var-file` flags, or
`TF_VAR_*` environment variables. `parser.ParseDir` is a separate path and reads every `*.tf`.
Symptom of this gap: a resource prints `unresolved: …` even though its value is
defined in a non-standard tfvars source. Extending resolution means teaching the resolver about
those sources explicitly — the parser already covers the `.tf` side.
