package mapper

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	ptypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/zclconf/go-cty/cty"

	"terraform-price/internal/parser"
	"terraform-price/internal/resolver"
)

type Kind int

const (
	KindFixed Kind = iota
	KindVariable
	KindFree
	KindUnsupported
)

var usageTypes = map[string]string{
	"aws_s3_bucket":               "S3 객체 저장·요청·데이터 전송 (usage 기반)",
	"aws_lambda_function":         "Lambda 요청·GB-초 (usage 기반)",
	"aws_sqs_queue":               "SQS 요청 (usage 기반)",
	"aws_sns_topic":               "SNS 요청 (usage 기반)",
	"aws_dynamodb_table":          "DynamoDB RCU/WCU 또는 on-demand (usage 기반)",
	"aws_cloudwatch_log_group":    "CloudWatch Logs 수집·저장 (usage 기반)",
	"aws_cloudfront_distribution": "CloudFront 데이터 전송·요청 (usage 기반)",
	"aws_apigateway_rest_api":     "API Gateway 요청 (usage 기반)",
	"aws_apigatewayv2_api":        "API Gateway 요청 (usage 기반)",
	"aws_codebuild_project":       "CodeBuild 빌드 분 (usage 기반)",
	"aws_kinesis_stream":          "Kinesis 샤드-시간+페이로드 (usage 기반)",
	"aws_efs_file_system":         "EFS GB-월, 동적 용량 (usage 기반)",
}

var infoTypes = map[string]string{
	"aws_launch_template":            "런치템플릿 — ASG/EKS 노드그룹 참조 시 산정",
	"aws_launch_configuration":       "런치템플릿 — ASG 참조 시 산정",
	"aws_docdb_cluster":              "DocumentDB 클러스터 — 인스턴스 비용은 aws_docdb_cluster_instance에서 산정",
	"aws_neptune_cluster":            "Neptune 클러스터 — 인스턴스 비용은 aws_neptune_cluster_instance에서 산정",
	"aws_vpc_endpoint":               "VPC endpoint — 서비스별 시간당 (단가 산정 안 됨)",
	"aws_vpn_connection":             "VPN 연결 — 시간당 (usagetype 미지원)",
	"aws_kms_key":                    "KMS 키 — $1/월 고정 (Price List 단가 unit 라이브 확인 필요)",
	"aws_secretsmanager_secret":      "Secrets Manager — $0.40/월 고정 (단가 unit 라이브 확인 필요)",
	"aws_route53_zone":               "Route53 호스티드존 — $0.50/월 Global (단가 라이브 확인 필요)",
	"aws_fsx_windows_file_system":    "FSx Windows — GB-월 (lustre만 산정)",
	"aws_fsx_ontap_file_system":      "FSx ONTAP — GB-월 (lustre만 산정)",
	"aws_fsx_openzfs_file_system":    "FSx OpenZFS — GB-월 (lustre만 산정)",
}

var freeTypes = map[string]struct{}{
	"aws_iam_role":                {},
	"aws_iam_policy":              {},
	"aws_iam_policy_attachment":   {},
	"aws_iam_user":                {},
	"aws_iam_group":               {},
	"aws_iam_instance_profile":    {},
	"aws_iam_access_key":          {},
	"aws_security_group":          {},
	"aws_security_group_rule":     {},
	"aws_vpc":                     {},
	"aws_subnet":                  {},
	"aws_route_table":             {},
	"aws_route":                   {},
	"aws_route_table_association": {},
	"aws_internet_gateway":        {},
	"aws_network_acl":             {},
	"aws_network_acl_rule":        {},
	"aws_network_interface":       {},
	"aws_db_subnet_group":         {},
}

type Spec struct {
	ServiceCode string
	Filters     []ptypes.Filter
	UsageQty    float64
	Count       int
	Label       string
	PreferUnit  string
}

var regionToLocation = map[string]string{
	"ap-northeast-2": "Asia Pacific (Seoul)",
	"ap-northeast-1": "Asia Pacific (Tokyo)",
	"ap-northeast-3": "Asia Pacific (Osaka)",
	"ap-southeast-1": "Asia Pacific (Singapore)",
	"ap-southeast-2": "Asia Pacific (Sydney)",
	"us-east-1":      "US East (N. Virginia)",
	"us-west-2":      "US West (Oregon)",
	"eu-west-1":      "Europe (Ireland)",
	"eu-central-1":   "Europe (Frankfurt)",
}

var regionToUsagePrefix = map[string]string{
	"ap-northeast-2": "APN2",
	"ap-northeast-1": "APN1",
	"ap-northeast-3": "APN3",
	"ap-southeast-1": "APS1",
	"ap-southeast-2": "APS2",
	"us-east-1":      "USE1",
	"us-west-2":      "USW2",
	"eu-west-1":      "EU",
	"eu-central-1":   "EUC1",
}

func tm(field, value string) ptypes.Filter {
	return ptypes.Filter{Type: ptypes.FilterTypeTermMatch, Field: aws.String(field), Value: aws.String(value)}
}

func usageType(region, base string) (string, bool) {
	p, ok := regionToUsagePrefix[region]
	if !ok {
		return "", false
	}
	return p + "-" + base, true
}

func resStr(r *parser.Resource, res *resolver.Resolver, key string) (string, bool) {
	expr, ok := r.Exprs[key]
	if !ok {
		return "", false
	}
	v, ok := res.ResolveExpr(expr)
	if !ok || !v.IsKnown() || v.IsNull() || v.Type() != cty.String {
		return "", false
	}
	return v.AsString(), true
}

func resNum(r *parser.Resource, res *resolver.Resolver, key string) (float64, bool) {
	expr, ok := r.Exprs[key]
	if !ok {
		return 0, false
	}
	v, ok := res.ResolveExpr(expr)
	if !ok || !v.IsKnown() || v.IsNull() || v.Type() != cty.Number {
		return 0, false
	}
	f, _ := v.AsBigFloat().Float64()
	return f, true
}

func resBool(r *parser.Resource, res *resolver.Resolver, key string) bool {
	expr, ok := r.Exprs[key]
	if !ok {
		return false
	}
	v, ok := res.ResolveExpr(expr)
	if !ok || !v.IsKnown() || v.IsNull() || v.Type() != cty.Bool {
		return false
	}
	return v.True()
}

func resListFirstStr(r *parser.Resource, res *resolver.Resolver, key string) (string, bool) {
	expr, ok := r.Exprs[key]
	if !ok {
		return "", false
	}
	v, ok := res.ResolveExpr(expr)
	if !ok || !v.IsKnown() || v.IsNull() {
		return "", false
	}
	t := v.Type()
	if !(t.IsListType() || t.IsSetType() || t.IsTupleType()) {
		return "", false
	}
	vals := v.AsValueSlice()
	if len(vals) == 0 || vals[0].Type() != cty.String {
		return "", false
	}
	return vals[0].AsString(), true
}

func lookupExpr(r *parser.Resource, keys []string) hcl.Expression {
	for _, k := range keys {
		if e, ok := r.Exprs[k]; ok {
			return e
		}
	}
	return nil
}

func refResource(expr hcl.Expression, idx map[string]*parser.Resource) *parser.Resource {
	ste, ok := expr.(*hclsyntax.ScopeTraversalExpr)
	if !ok {
		return nil
	}
	t := hcl.Traversal(ste.Traversal)
	if len(t) < 2 {
		return nil
	}
	root, ok := t[0].(hcl.TraverseRoot)
	if !ok {
		return nil
	}
	name, ok := t[1].(hcl.TraverseAttr)
	if !ok {
		return nil
	}
	return idx[root.Name+"."+name.Name]
}

func ec2InstanceSpec(it, loc, label string, count int) *Spec {
	return &Spec{
		ServiceCode: "AmazonEC2",
		Filters: []ptypes.Filter{
			tm("instanceType", it),
			tm("location", loc),
			tm("operatingSystem", "Linux"),
			tm("tenancy", "Shared"),
			tm("capacitystatus", "Used"),
			tm("preInstalledSw", "NA"),
		},
		UsageQty: 730, Count: count, Label: label,
	}
}

func MapResource(r *parser.Resource, res *resolver.Resolver, idx map[string]*parser.Resource, region string) (Kind, *Spec, string) {
	if desc, ok := usageTypes[r.Type]; ok {
		return KindVariable, nil, desc
	}
	if _, ok := freeTypes[r.Type]; ok {
		return KindFree, nil, "무과금 리소스"
	}
	if desc, ok := infoTypes[r.Type]; ok {
		return KindUnsupported, nil, desc
	}
	loc := regionToLocation[region]
	if loc == "" {
		return KindFixed, nil, "unknown region: " + region
	}
	var spec *Spec
	var note string
	var ok bool
	switch r.Type {
	case "aws_instance":
		spec, note, ok = mapEC2(r, res, loc)
	case "aws_db_instance":
		spec, note, ok = mapRDS(r, res, loc)
	case "aws_docdb_cluster_instance":
		spec, note, ok = mapDBInstance(r, res, loc, "AmazonDocDB")
	case "aws_neptune_cluster_instance":
		spec, note, ok = mapDBInstance(r, res, loc, "AmazonNeptune")
	case "aws_msk_cluster":
		spec, note, ok = mapMSK(r, res, loc)
	case "aws_elasticache_replication_group", "aws_elasticache_cluster":
		spec, note, ok = mapElastiCache(r, res, loc)
	case "aws_redshift_cluster":
		spec, note, ok = mapRedshift(r, res, loc)
	case "aws_opensearch_domain":
		spec, note, ok = mapOpenSearch(r, res, loc)
	case "aws_ebs_volume":
		spec, note, ok = mapEBS(r, res, loc)
	case "aws_autoscaling_group":
		spec, note, ok = mapASG(r, res, idx, loc)
	case "aws_eks_node_group":
		spec, note, ok = mapEKSNodeGroup(r, res, idx, loc)
	case "aws_nat_gateway":
		spec, note, ok = mapNATGateway(r, res, loc, region)
	case "aws_lb":
		spec, note, ok = mapLB(r, res, loc)
	case "aws_vpn_gateway":
		spec, note, ok = mapVPNGateway(r, res, loc, region)
	case "aws_fsx_lustre_filesystem":
		spec, note, ok = mapFSxLustre(r, res, loc, region)
	default:
		return KindUnsupported, nil, "미지원 리소스 (단가 산정 안 됨)"
	}
	if !ok {
		return KindFixed, nil, note
	}
	return KindFixed, spec, note
}

func mapEC2(r *parser.Resource, res *resolver.Resolver, loc string) (*Spec, string, bool) {
	it, ok := resStr(r, res, "instance_type")
	if !ok {
		return nil, "instance_type 미해석", false
	}
	tenancy := "Shared"
	if t, ok := resStr(r, res, "tenancy"); ok {
		tenancy = t
	}
	spec := ec2InstanceSpec(it, loc, it, 1)
	for i, f := range spec.Filters {
		if f.Field != nil && *f.Field == "tenancy" {
			spec.Filters[i] = tm("tenancy", tenancy)
		}
	}
	return spec, "", true
}

func mapRDS(r *parser.Resource, res *resolver.Resolver, loc string) (*Spec, string, bool) {
	ic, ok := resStr(r, res, "instance_class")
	if !ok {
		return nil, "instance_class 미해석", false
	}
	engine, _ := resStr(r, res, "engine")
	deploy := "Single-AZ"
	if resBool(r, res, "multi_az") {
		deploy = "Multi-AZ"
	}
	return &Spec{
		ServiceCode: "AmazonRDS",
		Filters: []ptypes.Filter{
			tm("instanceType", ic),
			tm("location", loc),
			tm("databaseEngine", rdsEngine(engine)),
			tm("deploymentOption", deploy),
		},
		UsageQty: 730, Count: 1, Label: ic,
	}, "", true
}

func mapDBInstance(r *parser.Resource, res *resolver.Resolver, loc, serviceCode string) (*Spec, string, bool) {
	ic, ok := resStr(r, res, "instance_class")
	if !ok {
		return nil, "instance_class 미해석", false
	}
	return &Spec{
		ServiceCode: serviceCode,
		Filters: []ptypes.Filter{
			tm("instanceType", ic),
			tm("location", loc),
			tm("deploymentOption", "Single-AZ"),
		},
		UsageQty: 730, Count: 1, Label: ic,
	}, "", true
}

func mapRedshift(r *parser.Resource, res *resolver.Resolver, loc string) (*Spec, string, bool) {
	it, ok := resStr(r, res, "node_type")
	if !ok {
		return nil, "node_type 미해석", false
	}
	count := 1
	if n, ok := resNum(r, res, "number_of_nodes"); ok && n > 0 {
		count = int(n)
	}
	return &Spec{
		ServiceCode: "AmazonRedshift",
		Filters: []ptypes.Filter{
			tm("instanceType", it),
			tm("location", loc),
		},
		UsageQty: 730, Count: count, Label: fmt.Sprintf("%s × %d nodes", it, count),
	}, "", true
}

func mapOpenSearch(r *parser.Resource, res *resolver.Resolver, loc string) (*Spec, string, bool) {
	it, ok := resStr(r, res, "cluster_config.instance_type")
	if !ok {
		return nil, "cluster_config.instance_type 미해석", false
	}
	count := 1
	if n, ok := resNum(r, res, "cluster_config.instance_count"); ok && n > 0 {
		count = int(n)
	}
	return &Spec{
		ServiceCode: "AmazonOpenSearchService",
		Filters: []ptypes.Filter{
			tm("instanceType", it),
			tm("location", loc),
		},
		UsageQty: 730, Count: count, Label: fmt.Sprintf("%s × %d nodes", it, count),
	}, "", true
}

func mapMSK(r *parser.Resource, res *resolver.Resolver, loc string) (*Spec, string, bool) {
	it, ok := resStr(r, res, "broker_node_group_info.instance_type")
	if !ok {
		return nil, "broker instance_type 미해석", false
	}
	computeFamily := strings.TrimPrefix(it, "kafka.")
	count := 1
	if n, ok := resNum(r, res, "number_of_broker_nodes"); ok && n > 0 {
		count = int(n)
	}
	return &Spec{
		ServiceCode: "AmazonMSK",
		Filters: []ptypes.Filter{
			tm("computeFamily", computeFamily),
			tm("operation", "RunBroker"),
			tm("location", loc),
		},
		UsageQty: 730, Count: count, Label: fmt.Sprintf("%s × %d brokers", it, count),
	}, "", true
}

func mapElastiCache(r *parser.Resource, res *resolver.Resolver, loc string) (*Spec, string, bool) {
	nt, ok := resStr(r, res, "node_type")
	if !ok {
		return nil, "node_type 미해석", false
	}
	engine := "Redis"
	if e, ok := resStr(r, res, "engine"); ok {
		engine = elasticacheEngine(e)
	}
	count := 1
	for _, k := range []string{"num_cache_clusters", "number_of_cache_clusters"} {
		if n, ok := resNum(r, res, k); ok && n > 0 {
			count = int(n)
			break
		}
	}
	return &Spec{
		ServiceCode: "AmazonElastiCache",
		Filters: []ptypes.Filter{
			tm("instanceType", nt),
			tm("location", loc),
			tm("cacheEngine", engine),
		},
		UsageQty: 730, Count: count, Label: fmt.Sprintf("%s × %d nodes", nt, count),
	}, "", true
}

func mapEBS(r *parser.Resource, res *resolver.Resolver, loc string) (*Spec, string, bool) {
	size, ok := resNum(r, res, "size")
	if !ok {
		return nil, "size 미해석", false
	}
	vtype := "gp3"
	if t, ok := resStr(r, res, "type"); ok {
		vtype = t
	}
	return &Spec{
		ServiceCode: "AmazonEC2",
		Filters: []ptypes.Filter{
			tm("volumeApiName", vtype),
			tm("location", loc),
		},
		UsageQty: size, Count: 1, Label: fmt.Sprintf("%s %gGB", vtype, size),
		PreferUnit: "GB-Mo",
	}, "", true
}

func mapASG(r *parser.Resource, res *resolver.Resolver, idx map[string]*parser.Resource, loc string) (*Spec, string, bool) {
	count := 0
	for _, k := range []string{"desired_capacity", "min_size"} {
		if n, ok := resNum(r, res, k); ok && n > 0 {
			count = int(n)
			break
		}
	}
	src := lookupExpr(r, []string{"launch_template.id", "launch_template_id", "launch_configuration"})
	var lt *parser.Resource
	if src != nil {
		lt = refResource(src, idx)
	}
	if lt == nil {
		return nil, "ASG launch_template/launch_configuration 참조 미해석", false
	}
	it, ok := resStr(lt, res, "instance_type")
	if !ok {
		return nil, "launch_template instance_type 미해석", false
	}
	if count == 0 {
		return nil, "ASG 용량(desired/min) 미해석", false
	}
	return ec2InstanceSpec(it, loc, fmt.Sprintf("%s × %d (ASG)", it, count), count), "", true
}

func mapEKSNodeGroup(r *parser.Resource, res *resolver.Resolver, idx map[string]*parser.Resource, loc string) (*Spec, string, bool) {
	count := 0
	if n, ok := resNum(r, res, "scaling_config.desired_size"); ok && n > 0 {
		count = int(n)
	}
	var it string
	if lt := lookupExpr(r, []string{"launch_template.id", "launch_template_name"}); lt != nil {
		if ltRes := refResource(lt, idx); ltRes != nil {
			if s, ok := resStr(ltRes, res, "instance_type"); ok {
				it = s
			}
		}
	}
	if it == "" {
		if s, ok := resListFirstStr(r, res, "instance_types"); ok {
			it = s
		}
	}
	if it == "" {
		return nil, "EKS node group instance_type 미해석 (launch_template/instance_types)", false
	}
	if count == 0 {
		return nil, "EKS node group desired_size 미해석", false
	}
	return ec2InstanceSpec(it, loc, fmt.Sprintf("%s × %d (EKS node)", it, count), count), "", true
}

func mapNATGateway(r *parser.Resource, res *resolver.Resolver, loc, region string) (*Spec, string, bool) {
	ut, ok := usageType(region, "NatGateway-Hours")
	if !ok {
		return nil, "NAT GW usagetype(리전) 미해석", false
	}
	return &Spec{
		ServiceCode: "AmazonEC2",
		Filters: []ptypes.Filter{
			tm("usagetype", ut),
			tm("location", loc),
		},
		UsageQty: 730, Count: 1, Label: "NAT Gateway", PreferUnit: "Hrs",
	}, "", true
}

func mapVPNGateway(r *parser.Resource, res *resolver.Resolver, loc, region string) (*Spec, string, bool) {
	ut, ok := usageType(region, "VPNGateway-Hours")
	if !ok {
		return nil, "VPN GW usagetype(리전) 미해석", false
	}
	return &Spec{
		ServiceCode: "AmazonEC2",
		Filters: []ptypes.Filter{
			tm("usagetype", ut),
			tm("location", loc),
		},
		UsageQty: 730, Count: 1, Label: "VPN Gateway", PreferUnit: "Hrs",
	}, "", true
}

func mapLB(r *parser.Resource, res *resolver.Resolver, loc string) (*Spec, string, bool) {
	lbt, ok := resStr(r, res, "load_balancer_type")
	if !ok || lbt == "" {
		return nil, "load_balancer_type 미해석", false
	}
	lbt = strings.ToUpper(lbt[:1]) + lbt[1:]
	return &Spec{
		ServiceCode: "AmazonElasticLoadBalancing",
		Filters: []ptypes.Filter{
			tm("loadBalancerType", lbt),
			tm("location", loc),
		},
		UsageQty: 730, Count: 1, Label: lbt + " LB", PreferUnit: "Hrs",
	}, "", true
}

func mapFSxLustre(r *parser.Resource, res *resolver.Resolver, loc, region string) (*Spec, string, bool) {
	size, ok := resNum(r, res, "storage_capacity")
	if !ok {
		return nil, "storage_capacity 미해석", false
	}
	ut, ok := usageType(region, "FSxLustre-Storage-GB-Mo")
	if !ok {
		return nil, "FSx usagetype(리전) 미해석", false
	}
	return &Spec{
		ServiceCode: "AmazonFSx",
		Filters: []ptypes.Filter{
			tm("fileSystemType", "LUSTRE"),
			tm("usagetype", ut),
			tm("location", loc),
		},
		UsageQty: size, Count: 1, Label: fmt.Sprintf("Lustre %gGB", size), PreferUnit: "GB-Mo",
	}, "", true
}

func rdsEngine(e string) string {
	switch e {
	case "mysql":
		return "MySQL"
	case "mariadb":
		return "MariaDB"
	case "postgres":
		return "PostgreSQL"
	case "oracle-se", "oracle-se1", "oracle-se2", "oracle-ee":
		return "Oracle"
	case "sqlserver-ex", "sqlserver-web", "sqlserver-se", "sqlserver-ee":
		return "SQL Server"
	}
	return "MySQL"
}

func elasticacheEngine(e string) string {
	switch e {
	case "memcached":
		return "Memcached"
	case "valkey":
		return "Valkey"
	}
	return "Redis"
}
