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

## Region dialect lives in awsprice — compose before the cache

`internal/provider/awsprice/regions.go` owns the AWS region vocabulary: `regionToLocation`,
`regionToUsagePrefix`, and the `usEast1UnprefixedUsagetypes` exception set. The tables were
derived from the live Price List API (`GetProducts AmazonEC2`, 2026-08-14) — not from memory —
because the vendor data has quirks: `eu-central-1`'s location string is `EU (Frankfurt)`, and a
few us-east-1 product families (Aurora storage, NAT gateway) ship unprefixed usagetypes while
others (Secrets Manager, RDS Proxy) are `USE1-` prefixed. Mappers carry the neutral region id in
`Spec.Region` / `Rate.Region` and pass unprefixed usagetype bases; `awsprice.Composer` injects the
location filter and the usagetype prefix. Wiring order in `buildPricer` is load-bearing: the
Composer must sit **above** the cache (`NewComposer(NewCached(client))`), never below — the cache
key is computed from the composed query, and a Composer below the cache would key on region-less
queries so different regions collide on one entry. `TestBuildPricerComposesBeforeCache` guards
the ordering; `TestCacheKeyFormatStable` guards the key rendering.

## Price cache is shared across AWS profiles — keep the profile out of the cache key

`internal/provider/cached.go` keys cached results by
`serviceCode | sorted spec filters (incl. the location filter awsprice composes) | preferUnit`
and intentionally omits the AWS profile. AWS OnDemand list prices are public and depend only on
region + resource spec, never on the querying account — discounts (RI / Savings Plan / EDP)
apply later at billing, not at the Price List API (see the caveat printed in `output.go`). One
cache file therefore correctly serves every profile; adding the profile to the key would be a
wrong "fix" that duplicates identical prices. TTL is 7 days (`provider.CacheTTL`);
`--no-cache` bypasses the cache for a run, and deleting
`$UserCacheDir/terraform-price/prices.json` forces a full refresh. The key string format is
load-bearing (existing cache files must keep hitting) — `TestCacheKeyFormatStable` guards it.

## Var / locals resolution sources — what is and is not read

`resolver.NewResolver` reads, in order: `terraform.tfvars`, then `*.auto.tfvars`
(later files win), then `variable` block defaults from `*.tf` files (unset vars
fall back to declared defaults), then `locals` blocks from every `*.tf` file,
resolved iteratively to a fixpoint — locals can reference other locals across
files and map iteration order is random, so a single pass intermittently leaves
locals unresolved (`TestLocalsAcrossFiles` guards this). Locals that still fail
reference computed resource attributes and stay pending until `SetResources` +
`RetryLocals` (analyze drives that loop with `RefResolver.Reset`). It does
**not** read `terraform.tfvars.json`, `-var` / `-var-file` flags, or `TF_VAR_*`
environment variables. `parser.ParseDir` is a separate path and reads every
`*.tf`. Symptom of the remaining gap: a resource prints `unresolved: …` even
though its value is defined in a non-standard var source.
