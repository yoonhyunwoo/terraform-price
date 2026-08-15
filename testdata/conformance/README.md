# Conformance fixtures

Vendored from [infracost/infracost](https://github.com/infracost/infracost)
`internal/providers/terraform/aws/testdata/` (109 cases, Apache-2.0 — see LICENSE).
Each `<name>_test/` directory holds a Terraform fixture (`*.tf`) and infracost's
golden output (`*.golden`). The `.usage.yml` files drive infracost's usage model
and are kept for provenance only; this tool does not read them.

Two consumers:

- `cmd/terraform-price/conformance_test.go` — the gate: runs the analyzer over
  every fixture with a stub pricer and compares the enumerated resource
  addresses against `snapshot.txt` (regenerate with -update). Parsing changes
  that drop or add rows fail CI.
- `cmd/conformance-sensor` — the sensor: same fixtures against live prices;
  reports dollar-level divergence from the goldens (usage defaults and price
  snapshot drift make this advisory, not a gate).
