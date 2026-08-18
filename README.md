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
| `--baseline` | — | Baseline directory to diff against (delta mode) |
| `[dir]` | `.` | Target Terraform directory (positional) |

Region comes from tfvars `aws_region` (default `ap-northeast-2`). All 39 regions the AWS Price List API covers are supported. The report title comes from tfvars `origin_service_name` (default: directory name).

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

### Credentials-free runs

Without AWS credentials the report still runs; price rows degrade to unresolved. For real
prices without giving CI any credentials, generate a price file once from a machine that
has them, commit it, and point the action at it:

terraform-price --price-file prices.json terraform/   # local, with credentials
git add prices.json
```

```yaml
      - uses: yoonhyunwoo/terraform-price@v0
        with:
          directory: terraform/
          baseline-ref: ${{ github.base_ref }}
          price-file: prices.json
```

The file uses the cache format, never expires, and grows: run the same command locally
whenever a new resource type appears so its prices are added. Lookups missing from the
file fall through to the network (and degrade to unresolved when CI has no credentials).

The report (and the delta table, when `baseline-ref` is set) is written to the job step summary and the log. Any AWS credentials work — the Price List API returns public list prices. The bulk price files under `~/.cache/terraform-price` are cached between runs. Resources that cannot be analyzed (`git::` / private module sources, unresolved references, price lookups without credentials) surface as not estimated rather than as a number.

## License

[MIT](LICENSE)
