<p align="center">
  <strong>terraform-price</strong>
</p>

<p align="center">
  <strong>Terraform 리소스별 AWS 월 단가를 코드에서 바로 추정하는 CLI.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat&colorA=222222&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Terraform-HCL-7B42BC?style=flat&colorA=222222&logo=terraform&logoColor=white" alt="Terraform">
  <img src="https://img.shields.io/badge/AWS-Price%20List%20API-FF9900?style=flat&colorA=222222&logo=amazonwebservices&logoColor=white" alt="AWS Price List API">
  <img src="https://img.shields.io/badge/단가-OnDemand%20정가-3FB950?style=flat&colorA=222222" alt="OnDemand">
  <img src="https://img.shields.io/badge/output-Markdown-58A6FF?style=flat&colorA=222222" alt="Markdown">
</p>

`apply` 하기 전에 이 디렉터리가 매달 얼마인지 알려준다. `*.tf`를 파싱하고, 변수·locals를 풀고, 각 리소스를 AWS Price List API의 실시간 OnDemand 단가에 매핑해 마크다운 보고서를 찍는다.

**20+** 과금 리소스 매핑 · **12** usage 리소스 · **9** 리전 · 단가는 **AWS Price List API** 라이브 · **7일** 디스크 캐시.

> OnDemand 정가 기준 — RI / Savings Plan / EDP 할인은 반영하지 않는다. 실제 청구액은 이 추정보다 낮다.

## 설치

소스에서 빌드·설치한다.

```sh
git clone <repo> && cd terraform-price
go install ./cmd/terraform-price
```

`$(go env GOPATH)/bin` 이 `PATH` 에 있으면 `terraform-price` 로 바로 실행된다. 빌드만 하려면 `go build -o terraform-price ./cmd/terraform-price`.

## 사용법

```sh
# 현재 디렉터리, 프로파일 지정
terraform-price --profile muhayu-hr

# 디렉터리 지정
terraform-price --profile muhayu-hr ./terraform/rds/monster/monsterp

# 단가 캐시 무시하고 새로 조회
terraform-price --profile muhayu-hr --no-cache
```

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `--profile` | tfvars `account_alias` | 단가 조회에 쓸 AWS 프로파일 |
| `--no-cache` | `false` | Price List API 결과 캐시를 무시하고 재조회 |
| `[dir]` | `.` | 대상 Terraform 디렉터리 (위치 인자) |

리전은 tfvars `aws_region` → 없으면 `ap-northeast-2`. 보고서 제목은 tfvars `origin_service_name` → 없으면 디렉터리명.

## 출력

표 네 개로 끝난다. 군더더기 텍스트 없음.

```markdown
# Cost Estimate — monsterp (`ap-northeast-2`)

> OnDemand 정가 기준 (RI / Savings Plan / EDP 할인 미반영)

## 고정비
| Resource | Spec | 단가 (USD) | 단위 | 월 (USD) |
|---|---|---:|---|---:|
| `aws_rds_cluster_instance.writer`   | db.r6i.large  | 0.3500 | Hrs | 255.50 |
| `aws_rds_cluster_instance.reader-1` | db.r8g.xlarge | 0.8660 | Hrs | 632.18 |
| **고정비 합계 / 월** | | | | **887.68** |

## 유동비
| Resource | 유형 | 단가 (USD) | 비고 |
|---|---|---|---|
| `aws_rds_cluster.default` | aws_rds_cluster | 스토리지 0.1200/GB-월 · I/O 0.2400/100만 I/O | Aurora 스토리지·I/O — 사용량만큼 청구 |

## 무료
| Resource | 유형 |
|---|---|
| `aws_security_group.default` | aws_security_group |
| `random_password.instance_password` | random_password |
```

- **고정비** — 시간당·GB-월처럼 사용량이 정해진 리소스. 단가 × usage(730h / GB·월) × 수 = 월 비용. 합계가 나온다.
- **유동비** — 요청·전송량처럼 usage에 비례하는 리소스. 알 수 있는 건 단위 단가까지(예: Aurora 스토리지 $/GB-월), 월 합계에는 넣지 않는다.
- **⚠️ 미지원** — 과금되지만 아직 매핑이 없는 리소스. 누락이 보이게 별도 표시.
- **무료** — IAM·SG·VPC·파라미터 그룹처럼 과금 없는 리소스.

## 지원 리소스

### 고정비 — 단가 산정

| 리소스 | 서비스 | 핵심 속성 |
|---|---|---|
| `aws_instance` | EC2 | `instance_type`, `tenancy` |
| `aws_db_instance` | RDS | `instance_class`, `engine`, `multi_az` |
| `aws_rds_cluster_instance` | Aurora | `instance_class`, `engine`(또는 클러스터에서) |
| `aws_docdb_cluster_instance` | DocumentDB | `instance_class` |
| `aws_neptune_cluster_instance` | Neptune | `instance_class` |
| `aws_elasticache_replication_group` · `aws_elasticache_cluster` | ElastiCache | `node_type`, `engine`, 노드 수 |
| `aws_redshift_cluster` | Redshift | `node_type`, `number_of_nodes` |
| `aws_opensearch_domain` | OpenSearch | `cluster_config.instance_type`/`instance_count` |
| `aws_msk_cluster` | MSK | `broker_node_group_info.instance_type`, 브로커 수 |
| `aws_ebs_volume` | EBS | `type`, `size` (GB-월) |
| `aws_autoscaling_group` | EC2 | launch template 참조 → `instance_type` × 용량 |
| `aws_eks_node_group` | EC2 | launch template / `instance_types` × `desired_size` |
| `aws_nat_gateway` | EC2 | 리전별 시간당 |
| `aws_lb` | ELB | `load_balancer_type` (ALB/NLB/GWLB) |
| `aws_vpn_gateway` | EC2 | 리전별 시간당 |
| `aws_fsx_lustre_filesystem` | FSx | `storage_capacity` (GB-월) |
| `aws_secretsmanager_secret` | Secrets Manager | $0.40/secret·월 |
| `aws_db_proxy` | RDS Proxy | 대상 인스턴스 vCPU 자동 합산 × 시간당 |

### 유동비 — 단위 단가만 (월 합계 제외)

`aws_rds_cluster`(Aurora 스토리지·I/O, 단가 표시) · `aws_db_proxy`(vCPU 미해석 시 fallback) · `aws_s3_bucket` · `aws_lambda_function` · `aws_sqs_queue` · `aws_sns_topic` · `aws_dynamodb_table` · `aws_cloudwatch_log_group` · `aws_cloudfront_distribution` · `aws_apigateway_rest_api` · `aws_apigatewayv2_api` · `aws_codebuild_project` · `aws_kinesis_stream` · `aws_efs_file_system`

### 무료 — 과금 없음

IAM(role/policy/user/group/instance_profile/access_key) · Security Group(+rule) · VPC · Subnet · Route Table(+route/association) · Internet Gateway · Network ACL(+rule) · ENI · DB Subnet Group · Parameter/Option Group(rds/db) · Proxy Target(+default_target_group) · Secret Version · `random_*` · `null_resource`

### 리전

`ap-northeast-2` · `ap-northeast-1` · `ap-northeast-3` · `ap-southeast-1` · `ap-southeast-2` · `us-east-1` · `us-west-2` · `eu-west-1` · `eu-central-1`

## 단가 캐시

조회 결과는 `$UserCacheDir/terraform-price/prices.json` 에 **7일** 캐시된다. 캐시 키는 `serviceCode | region | spec filters | preferUnit` — **AWS 프로파일은 키에 넣지 않는다.** OnDemand 정가는 공개 단가라 조회 계정과 무관하기 때문이다. 캐시 파일 하나가 모든 프로파일을 커버한다.

- `--no-cache` — 이번 실행만 캐시 우회
- 캐시 파일 삭제 — 전체 재조회

## 변수 해석

`terraform.tfvars` 와 `locals.tf` 두 파일만 읽는다. `*.auto.tfvars`, `terraform.tfvars.json`, `-var` / `-var-file`, `TF_VAR_*` 는 보지 않는다. 비표준 소스에 값이 있으면 해당 리소스는 `⚠️ … 미해석` 으로 뜬다. (`.tf` 쪽은 파서가 전부 읽는다.)

## 구조

| 패키지 | 역할 |
|---|---|
| `cmd/terraform-price` | CLI 진입점 · 플래그 · 보고서 조립 |
| `internal/parser` | `*.tf` 파싱 → 리소스 + 표현식 |
| `internal/resolver` | `terraform.tfvars` · `locals.tf` 에서 var/locals 해석 |
| `internal/mapper` | 리소스 → AWS Price List 필터(Spec) 매핑 |
| `internal/price` | Price List API 호출 · 단가 추출 |
| `internal/price` (cached) | 7일 디스크 캐시 |
| `internal/output` | 마크다운 보고서 렌더 |

```sh
go build ./...
go test ./...
```

## 한계

- **OnDemand 정가만** — 협상 할인(RI/SP/EDP)은 반영 안 함.
- **유동비는 단가까지** — 요청·전송량을 알 수 없으니 월 합계에 넣지 않는다.
- **인스턴스 vCPU 추정** — RDS Proxy 비용은 인스턴스 class 이름 규칙으로 vCPU를 계산한다. 현행 세대(t3/t4g/r/m) 기준이라 legacy t2 타겟은 어긋날 수 있다.
- **변수 해석 범위** — 위 "변수 해석" 참고.
