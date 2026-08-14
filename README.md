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

## Usage

```sh
terraform-price --profile muhayu-hr ./terraform/rds/monster/monsterp
```

| Flag | Default | Description |
|---|---|---|
| `--profile` | tfvars `account_alias` | AWS profile for price lookups |
| `--no-cache` | `false` | Bypass the price cache for this run |
| `[dir]` | `.` | Target Terraform directory (positional) |

Region comes from tfvars `aws_region` (default `ap-northeast-2`). The report title comes from tfvars `origin_service_name` (default: directory name).

## Report

Four tables:

- **Fixed** — usage fully determined by the spec (730 hours, or GB × count). Monthly = unit price × usage × count, summed into a total. Covers `aws_instance`, `aws_db_instance`, Aurora / DocumentDB / Neptune instances, ElastiCache, Redshift, OpenSearch, MSK, EBS, ASG, EKS node groups, NAT / VPN gateway, `aws_lb`, FSx Lustre, Secrets Manager, RDS Proxy (target vCPU summed automatically).
- **Variable** — billed by requests or transfer, which code cannot know. Unit rates only (e.g. Aurora storage $/GB-mo), never in the total. Covers `aws_rds_cluster` storage & I/O, S3, Lambda, SQS, SNS, DynamoDB, CloudWatch Logs, CloudFront, API Gateway, CodeBuild, Kinesis, EFS.
- **Unsupported** — billable but not mapped yet, listed separately so gaps stay visible. VPC endpoint, VPN connection, KMS key, Route53 zone, non-Lustre FSx, launch templates (priced via the ASG/EKS node group referencing them), and more.
- **Free** — IAM, security groups, VPC, subnets, route tables, network ACLs, ENI, DB subnet / parameter / option groups, `random_*`, `null_resource`.

Regions: `ap-northeast-2/1/3` · `ap-southeast-1/2` · `us-east-1` · `us-west-2` · `eu-west-1` · `eu-central-1`.

## Notes

- **Price cache** — 7 days at `$UserCacheDir/terraform-price/prices.json`. The cache key omits the AWS profile: OnDemand list prices are public, so one file serves every profile. Delete the file to force a full refresh.
- **Variable resolution** — reads only `terraform.tfvars` and `locals.tf`. Not `*.auto.tfvars`, `terraform.tfvars.json`, `-var` / `-var-file`, or `TF_VAR_*`; affected resources print as unresolved.
- **RDS Proxy vCPU** — derived from instance-class naming, following current-generation classes (t3/t4g/r/m). Legacy t2 targets may differ.

## License

[MIT](LICENSE)
