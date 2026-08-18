<p align="center">
  <strong>terraform-price</strong>
</p>

<p align="center">
  <strong>See the monthly AWS bill for a Terraform directory — before you apply it.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat&colorA=222222&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Terraform-HCL-7B42BC?style=flat&colorA=222222&logo=terraform&logoColor=white" alt="Terraform">
  <img src="https://img.shields.io/badge/AWS-Price%20List%20API-FF9900?style=flat&colorA=222222&logo=amazonwebservices&logoColor=white" alt="AWS Price List API">
  <img src="https://img.shields.io/badge/pricing-OnDemand%20list-3FB950?style=flat&colorA=222222" alt="OnDemand">
  <img src="https://img.shields.io/badge/output-Markdown-58A6FF?style=flat&colorA=222222" alt="Markdown">
</p>

terraform-price parses `*.tf`, resolves `var`/`local` references, maps each resource to a live AWS Price List OnDemand rate, and prints a Markdown cost report. Run it in the directory you are about to `apply`.

**20+** priced resource types · **12** usage-based types · **9** regions · live **AWS Price List API** rates · **7-day** disk cache.

> OnDemand list prices only — RI / Savings Plan / EDP discounts are not applied. Your real bill is lower than this estimate.

## Install

```sh
go install github.com/yoonhyunwoo/terraform-price/cmd/terraform-price@latest
```

Or download a prebuilt binary from [releases](https://github.com/yoonhyunwoo/terraform-price/releases).

For CI, use the [GitHub Action](#github-action) instead.

## Usage

```sh
```

| Flag | Default | Description |
|---|---|---|
| `--no-cache` | `false` | Bypass the price cache for this run |
| `--price-file` | — | JSON price file seeding lookups (never expires; misses fall through to the network and hits are written back) |
| `--format` | `full` | `full` (all tables) or `compact` (CI summary) |
| `--baseline` | — | Baseline directory to diff against (delta mode) |
| `--lang` | `en` | Report language: `en`, `ko` (also `TFPRICE_LANG`, `LC_ALL`, `LC_MESSAGES`, `LANG`) |
| `[dir]` | `.` | Target Terraform directory (positional) |

Region comes from tfvars `aws_region` (default `us-east-1`, the Price List API home region). All 39 regions the AWS Price List API covers are supported. The report title comes from tfvars `origin_service_name` (default: directory name).

## Languages

Reports are available in English (source) and Korean. Language resolution:
`--lang` flag, then `TFPRICE_LANG`, `LC_ALL`, `LC_MESSAGES`, `LANG`; an
unsupported value falls back to English.

### Adding a translation

1. Copy `internal/i18n/active.en.json` to `internal/i18n/active.<code>.json`
   and translate the values. Keep the keys, the `{{.Placeholder}}` names, and
   resource specs (`t3.medium`, `gp3 500GB`) unchanged.
2. Add the code to `Languages` in `internal/i18n/i18n.go`.
3. Run `go test ./internal/i18n/` — `TestLocalesComplete` fails on missing
   keys, unknown keys, or placeholders dropped in translation (the same check
   CI runs on your PR).

English is the source of truth: rewording an English message is a deliberate
change that every locale file must follow (the test enforces the sync).

## Report

Four tables:

- **Fixed** — usage fully determined by the spec (730 hours, or GB × count). Monthly = unit price × usage × count, summed into a total. Covers `aws_instance`, `aws_db_instance`, Aurora / DocumentDB / Neptune instances, ElastiCache, Redshift, OpenSearch, MSK, EBS, ASG, EKS node groups, NAT / VPN gateway, `aws_lb`, FSx Lustre, Secrets Manager, RDS Proxy (target vCPU summed automatically).
- **Variable** — billed by requests or transfer, which code cannot know. Unit rates only (e.g. Aurora storage $/GB-mo), never in the total. Covers `aws_rds_cluster` storage & I/O, S3, Lambda, SQS, SNS, DynamoDB, CloudWatch Logs, CloudFront, API Gateway, CodeBuild, Kinesis, EFS.
- **Unsupported** — billable but not mapped yet, listed separately so gaps stay visible. VPC endpoint, VPN connection, KMS key, Route53 zone, non-Lustre FSx, launch templates (priced via the ASG/EKS node group referencing them), and more.
- **Free** — IAM, security groups, VPC, subnets, route tables, network ACLs, ENI, DB subnet / parameter / option groups, `random_*`, `null_resource`.

With `--baseline`, a `Delta vs baseline` section is appended: per-resource prior / proposed / delta rows (create, delete, update), with usage-based, unsupported, and unresolved resources listed as not estimated instead of silently dropped. The baseline is what that directory declares — not live state — so drift between state and code is not reflected.

## Notes

- **Price cache** — 7 days at `$UserCacheDir/terraform-price/prices.json`. The cache key omits the AWS profile: OnDemand list prices are public, so one file serves every profile. Delete the file to force a full refresh.
- **Variable resolution** — reads `terraform.tfvars`, then `*.auto.tfvars` (later wins), `variable` defaults from `*.tf`, and `locals` from every `*.tf` (fixpoint). Not `terraform.tfvars.json`, `-var` / `-var-file`, or `TF_VAR_*`; affected resources print as unresolved.
- **count / for_each** — a resolvable `count` or literal `for_each` multiplies the estimate (`× N` in the spec column); unresolvable ones are priced as one and flagged inline.
- **Modules** — local-path and public-registry modules (`ns/name/provider`, version-pinned via the block's `version`) are expanded recursively with their input values; registry tarballs are cached under `$UserCacheDir/terraform-price/modules/`. `git::` / private / unfetchable sources stay under Unsupported. References to module outputs (`module.x.out`) are not resolved.
- **Usage-based fees** — data transfer and per-GB processing (e.g. NAT Gateway) are not in the Fixed total; only fixed hourly/GB-month dimensions are.
- **RDS Proxy vCPU** — derived from instance-class naming (current-gen and legacy t2).

## GitHub Action

Add a cost report to pull requests with one step:

```yaml
name: cost
on: pull_request
permissions:
  contents: read
  pull-requests: write
jobs:
  cost:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: aws-actions/configure-aws-credentials@v5
        with:
          role-to-assume: ${{ secrets.COST_REPORT_ROLE_ARN }}
          aws-region: us-east-1
      - uses: yoonhyunwoo/terraform-price@v0
        with:
          directory: terraform/
          baseline-ref: ${{ github.base_ref }}
```

Inputs:

| Input | Default | Description |
|---|---|---|
| `directory` | `.` | Terraform directory (PR head already checked out) |
| `baseline-ref` | — | Ref to diff against; empty skips the delta section |
| `price-file` | — | Committed `prices.json` for credentials-free runs |
| `version` | `latest` | Release tag to install |
| `format` | `compact` | `compact` (delta-first CI summary) or `full` (all tables) |
| `language` | — | Report language, e.g. `ko`; empty follows the runner's `LANG` |

### Credentials-free runs

The binary ships with an embedded price catalog (common EC2/RDS/ElastiCache/
DocumentDB/Neptune/MSK/Redshift/OpenSearch types, EBS volumes, NAT gateways,
VPN, NLB, Secrets Manager, KMS, WAF, EKS — across 10 major regions), so a
run with **no AWS credentials at all** still reports real prices for common
resources. Any fresher source wins over the catalog: a `--price-file`,
credentials enabling the live Price List API, then the catalog last. When the
catalog answers, the report says so and gives its generation date.

The catalog is generated by running terraform-price itself over a synthetic
Terraform file, once per region:

```sh
make catalog    # needs AWS credentials; writes internal/catalog/prices.json
```

The curated resource list lives in `testdata/catalog/main.tf` — add a
resource there (and a region to `CATALOG_REGIONS` if needed) and re-run
`make catalog` to extend offline coverage. Coverage outside the catalog
degrades to not-estimated unless credentials are available.

The `price-file` input/flag still works for exact coverage of a specific
stack: commit a generated `prices.json` and point the action at it. It never
expires and takes precedence over the embedded catalog.

On `pull_request` events the report is also posted as a PR comment (updated in place on re-runs) — the workflow needs `pull-requests: write` for that. The report (and the delta table, when `baseline-ref` is set) is written to the job step summary and the log. Any AWS credentials work — the Price List API returns public list prices. The bulk price files under `~/.cache/terraform-price` are cached between runs. Resources that cannot be analyzed (`git::` / private module sources, unresolved references, price lookups without credentials) surface as not estimated rather than as a number.

## License

[MIT](LICENSE)
