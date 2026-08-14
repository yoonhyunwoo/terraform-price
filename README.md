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

terraform-price parses `*.tf`, resolves `var` and `local` references, maps each resource to a live AWS Price List OnDemand rate, and prints a Markdown cost report. Run it in the directory you are about to `apply` and read the four tables it prints.

**20+** priced resource types · **12** usage-based types · **9** regions · live **AWS Price List API** rates · **7-day** disk cache.

> OnDemand list prices only — RI / Savings Plan / EDP discounts are not applied. Your real bill is lower than this estimate.

## Install

Build and install from source.

```sh
git clone https://github.com/yoonhyunwoo/terraform-price && cd terraform-price
go install ./cmd/terraform-price
```

If `$(go env GOPATH)/bin` is on your `PATH`, the `terraform-price` command is ready to run. To build without installing, run `go build -o terraform-price ./cmd/terraform-price`.

## Usage

Point the command at a Terraform directory.

```sh
# current directory, explicit profile
terraform-price --profile muhayu-hr

# explicit directory
terraform-price --profile muhayu-hr ./terraform/rds/monster/monsterp

# bypass the price cache for this run
terraform-price --profile muhayu-hr --no-cache
```

| Flag | Default | Description |
|---|---|---|
| `--profile` | tfvars `account_alias` | AWS profile used for price lookups |
| `--no-cache` | `false` | Bypass the Price List API cache for this run |
| `[dir]` | `.` | Target Terraform directory (positional) |

Region comes from tfvars `aws_region`, falling back to `ap-northeast-2`. The report title comes from tfvars `origin_service_name`, falling back to the directory name.

## Output

One report, four tables, no filler.

```markdown
# Cost Estimate — monsterp (`ap-northeast-2`)

> OnDemand list prices only — RI / Savings Plan / EDP discounts not applied

## Fixed
| Resource | Spec | Unit price (USD) | Unit | Monthly (USD) |
|---|---|---:|---|---:|
| `aws_rds_cluster_instance.writer`   | db.r6i.large  | 0.3500 | Hrs | 255.50 |
| `aws_rds_cluster_instance.reader-1` | db.r8g.xlarge | 0.8660 | Hrs | 632.18 |
| **Fixed total / month** | | | | **887.68** |

## Variable
| Resource | Type | Unit price (USD) | Notes |
|---|---|---|---|
| `aws_rds_cluster.default` | aws_rds_cluster | storage 0.1200/GB-mo · I/O 0.2400/1M I/O | Aurora storage & I/O — billed by usage |

## Unsupported
| Resource | Type | Notes |
|---|---|---|
| `aws_vpc_endpoint.s3` | aws_vpc_endpoint | VPC endpoint — per-service hourly (not priced yet) |

## Free
| Resource | Type |
|---|---|
| `aws_security_group.default` | aws_security_group |
| `random_password.instance_password` | random_password |
```

- **Fixed** — usage is fully determined by the spec (730 hours, or GB × count). Monthly = unit price × usage × count, and these rows sum into the total.
- **Variable** — billed by request or transfer volume, which the code cannot know. The report shows the unit rate (for example Aurora storage $/GB-mo) and keeps these out of the monthly total.
- **Unsupported** — billable, but no mapping exists yet. Listed separately so pricing gaps stay visible instead of silently disappearing.
- **Free** — resources that never incur charges: IAM, security groups, VPC, parameter groups.

## Supported resources

### Fixed — priced

| Resource | Service | Key attributes |
|---|---|---|
| `aws_instance` | EC2 | `instance_type`, `tenancy` |
| `aws_db_instance` | RDS | `instance_class`, `engine`, `multi_az` |
| `aws_rds_cluster_instance` | Aurora | `instance_class`, `engine` (or from the cluster) |
| `aws_docdb_cluster_instance` | DocumentDB | `instance_class` |
| `aws_neptune_cluster_instance` | Neptune | `instance_class` |
| `aws_elasticache_replication_group` · `aws_elasticache_cluster` | ElastiCache | `node_type`, `engine`, node count |
| `aws_redshift_cluster` | Redshift | `node_type`, `number_of_nodes` |
| `aws_opensearch_domain` | OpenSearch | `cluster_config.instance_type`/`instance_count` |
| `aws_msk_cluster` | MSK | `broker_node_group_info.instance_type`, broker count |
| `aws_ebs_volume` | EBS | `type`, `size` (GB-month) |
| `aws_autoscaling_group` | EC2 | launch template reference → `instance_type` × capacity |
| `aws_eks_node_group` | EC2 | launch template / `instance_types` × `desired_size` |
| `aws_nat_gateway` | EC2 | hourly, per region |
| `aws_lb` | ELB | `load_balancer_type` (ALB/NLB/GWLB) |
| `aws_vpn_gateway` | EC2 | hourly, per region |
| `aws_fsx_lustre_filesystem` | FSx | `storage_capacity` (GB-month) |
| `aws_secretsmanager_secret` | Secrets Manager | $0.40/secret·month |
| `aws_db_proxy` | RDS Proxy | target instance vCPU summed automatically × hourly |

### Variable — unit rates only, excluded from the total

`aws_rds_cluster` (Aurora storage & I/O, rates shown) · `aws_db_proxy` (fallback when vCPU is unresolved) · `aws_s3_bucket` · `aws_lambda_function` · `aws_sqs_queue` · `aws_sns_topic` · `aws_dynamodb_table` · `aws_cloudwatch_log_group` · `aws_cloudfront_distribution` · `aws_apigateway_rest_api` · `aws_apigatewayv2_api` · `aws_codebuild_project` · `aws_kinesis_stream` · `aws_efs_file_system`

### Free — no charge

IAM (role/policy/user/group/instance_profile/access_key) · Security Group (+rule) · VPC · Subnet · Route Table (+route/association) · Internet Gateway · Network ACL (+rule) · ENI · DB Subnet Group · Parameter/Option Group (rds/db) · Proxy Target (+default_target_group) · Secret Version · `random_*` · `null_resource`

### Regions

`ap-northeast-2` · `ap-northeast-1` · `ap-northeast-3` · `ap-southeast-1` · `ap-southeast-2` · `us-east-1` · `us-west-2` · `eu-west-1` · `eu-central-1`

## Price cache

Lookups are cached for **7 days** at `$UserCacheDir/terraform-price/prices.json`. The cache key is `serviceCode | region | spec filters | preferUnit` — the AWS **profile is deliberately not part of the key**: OnDemand list prices are public and identical for every account, so one cache file serves all profiles.

- `--no-cache` — bypass the cache for a single run
- delete the cache file — force a full refresh

## Variable resolution

The resolver reads only `terraform.tfvars` and `locals.tf`. It does not read `*.auto.tfvars`, `terraform.tfvars.json`, `-var` / `-var-file`, or `TF_VAR_*`. A resource whose value lives in one of those sources prints as `unresolved`. All `*.tf` files are parsed regardless.

## Package layout

| Package | Role |
|---|---|
| `cmd/terraform-price` | CLI entry point, flags, report assembly |
| `internal/parser` | parse `*.tf` → resources + expressions |
| `internal/resolver` | resolve `var`/`locals` from `terraform.tfvars` and `locals.tf` |
| `internal/mapper` | resource → AWS Price List filter (`Spec`) mapping |
| `internal/price` | Price List API calls, price extraction |
| `internal/price` (cached) | 7-day disk cache |
| `internal/output` | Markdown report rendering |

```sh
go build ./...
go test ./...
```

## Limitations

- **OnDemand list prices only** — negotiated discounts (RI/SP/EDP) are not applied.
- **Variable costs stop at unit rates** — request and transfer volumes cannot be known, so they never enter the monthly total.
- **Instance vCPU estimation** — RDS Proxy cost derives vCPU from instance-class naming. It follows current-generation classes (t3/t4g/r/m); legacy t2 targets may differ.
- **Variable resolution scope** — see [Variable resolution](#variable-resolution).

## License

[MIT](LICENSE)
