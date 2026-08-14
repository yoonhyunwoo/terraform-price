package mapper

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/yoonhyunwoo/terraform-price/internal/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/provider"
	"github.com/yoonhyunwoo/terraform-price/internal/resolver"
)

type Kind int

const (
	KindFixed Kind = iota
	KindVariable
	KindFree
	KindUnsupported
)

var usageTypes = map[string]string{
	"aws_s3_bucket":                             "S3 storage, requests, and data transfer (usage-based)",
	"aws_lambda_function":                       "Lambda requests and GB-seconds (usage-based)",
	"aws_sqs_queue":                             "SQS requests (usage-based)",
	"aws_sns_topic":                             "SNS requests (usage-based)",
	"aws_dynamodb_table":                        "DynamoDB RCU/WCU or on-demand (usage-based)",
	"aws_cloudwatch_log_group":                  "CloudWatch Logs ingestion and storage (usage-based)",
	"aws_cloudfront_distribution":               "CloudFront data transfer and requests (usage-based)",
	"aws_apigateway_rest_api":                   "API Gateway requests (usage-based)",
	"aws_elasticache_serverless_cache":          "ElastiCache serverless: ECPU + data storage GB-month (usage-based)",
	"aws_lb_listener":                           "ALB/NLB LCU or data processing (usage-based)",
	"aws_lambda_provisioned_concurrency_config": "Provisioned concurrency GB-hours (memory size × concurrency, usage-based)",
	"aws_s3_directory_bucket":                   "S3 Express One Zone storage and requests (usage-based)",
	"aws_apigatewayv2_api":                      "API Gateway requests (usage-based)",
	"aws_codebuild_project":                     "CodeBuild build minutes (usage-based)",
	"aws_kinesis_stream":                        "Kinesis shard-hours plus payload (usage-based)",
	"aws_efs_file_system":                       "EFS GB-month, dynamic size (usage-based)",
}

var infoTypes = map[string]string{
	"aws_ec2_transit_gateway":     "transit gateway — billed per attachment (see aws_ec2_transit_gateway_vpc_attachment)",
	"aws_launch_template":         "launch template — priced via the ASG/EKS node group referencing it",
	"aws_launch_configuration":    "launch config — priced via the ASG referencing it",
	"aws_docdb_cluster":           "DocumentDB cluster — instance cost is priced via aws_docdb_cluster_instance",
	"aws_neptune_cluster":         "Neptune cluster — instance cost is priced via aws_neptune_cluster_instance",
	"aws_vpc_endpoint":            "VPC endpoint — per-service hourly (not priced yet)",
	"aws_kms_key":                 "KMS key — $1/month flat (Price List unit needs live confirmation)",
	"aws_route53_zone":            "Route53 hosted zone — $0.50/month global (price needs live confirmation)",
	"module":                      "module source not analyzable (git::/private/unfetchable) — resources inside are not estimated",
	"aws_fsx_windows_file_system": "FSx Windows — GB-month (only Lustre is priced)",
	"aws_fsx_ontap_file_system":   "FSx ONTAP — GB-month (only Lustre is priced)",
	"aws_fsx_openzfs_file_system": "FSx OpenZFS — GB-month (only Lustre is priced)",
}

var freeTypes = map[string]struct{}{
	"aws_ecs_capacity_provider":          {},
	"aws_ecs_cluster_capacity_providers": {},
	"aws_iam_role":                       {},
	"aws_iam_policy":                     {},
	"aws_iam_policy_attachment":          {},
	"aws_iam_user":                       {},
	"aws_iam_group":                      {},
	"aws_iam_instance_profile":           {},
	"aws_iam_access_key":                 {},
	"aws_security_group":                 {},
	"aws_security_group_rule":            {},
	"aws_vpc":                            {},
	"aws_subnet":                         {},
	"aws_route_table":                    {},
	"aws_route":                          {},
	"aws_route_table_association":        {},
	"aws_internet_gateway":               {},
	"aws_network_acl":                    {},
	"aws_network_acl_rule":               {},
	"aws_network_interface":              {},
	"aws_db_subnet_group":                {},
	"aws_iam_role_policy_attachment":     {},
	"aws_iam_role_policy":                {},
	"aws_rds_cluster_parameter_group":    {},
	"aws_db_parameter_group":             {},
	"aws_db_option_group":                {},
	"aws_db_proxy_default_target_group":  {},
	"aws_db_proxy_target":                {},
	"aws_secretsmanager_secret_version":  {},
	"random_password":                    {},
	"random_string":                      {},
	"random_id":                          {},
	"null_resource":                      {},
}

type Spec struct {
	ServiceCode string
	Region      string
	Filters     []provider.Filter
	UsageQty    float64
	Count       int
	Label       string
	Note        string
	PreferUnit  string
	Rates       []Rate
}

type Rate struct {
	Label       string
	ServiceCode string
	Region      string
	Filters     []provider.Filter
	PreferUnit  string
	DisplayMult float64
	DisplayUnit string
}

func tm(field, value string) provider.Filter {
	return provider.Filter{Field: field, Value: value}
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

func ec2InstanceSpec(it, label string, count int) *Spec {
	return &Spec{
		ServiceCode: "AmazonEC2",
		Filters: []provider.Filter{
			tm("instanceType", it),
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
		return KindFree, nil, "no-charge resource"
	}
	if desc, ok := infoTypes[r.Type]; ok {
		return KindUnsupported, nil, desc
	}
	kind := KindFixed
	var spec *Spec
	var note string
	var ok bool
	switch r.Type {
	case "aws_instance":
		spec, note, ok = mapEC2(r, res)
	case "aws_db_instance":
		spec, note, ok = mapRDS(r, res)
	case "aws_rds_cluster":
		spec, note, ok = mapAuroraCluster(r, res)
		kind = KindVariable
	case "aws_db_proxy":
		spec, note, ok = mapDBProxy(r, res, idx)
		if ok && len(spec.Rates) == 0 {
			kind = KindFixed
		} else {
			kind = KindVariable
		}
	case "aws_secretsmanager_secret":
		spec, note, ok = mapSecret(r, res)
	case "aws_rds_cluster_instance":
		spec, note, ok = mapAuroraInstance(r, res, idx)
	case "aws_docdb_cluster_instance":
		spec, note, ok = mapDBInstance(r, res, "AmazonDocDB")
	case "aws_neptune_cluster_instance":
		spec, note, ok = mapDBInstance(r, res, "AmazonNeptune")
	case "aws_msk_cluster":
		spec, note, ok = mapMSK(r, res)
	case "aws_eks_cluster":
		spec, note, ok = flatHourly("AmazonEKS", "AmazonEKS-Hours:perCluster", "EKS control plane"), "", true
	case "aws_vpn_connection":
		spec, note, ok = flatHourly("AmazonVPC", "VPN-Usage-Hours:ipsec.1", "VPN connection"), "", true
	case "aws_ec2_transit_gateway_vpc_attachment":
		spec, note, ok = flatHourly("AmazonVPC", "TransitGateway-Hours", "TGW VPC attachment"), "", true
	case "aws_elasticache_replication_group", "aws_elasticache_cluster":
		spec, note, ok = mapElastiCache(r, res)
	case "aws_redshift_cluster":
		spec, note, ok = mapRedshift(r, res)
	case "aws_opensearch_domain":
		spec, note, ok = mapOpenSearch(r, res)
	case "aws_ebs_volume":
		spec, note, ok = mapEBS(r, res)
	case "aws_autoscaling_group":
		spec, note, ok = mapASG(r, res, idx)
	case "aws_eks_node_group":
		spec, note, ok = mapEKSNodeGroup(r, res, idx)
	case "aws_nat_gateway":
		spec, note, ok = mapNATGateway(r, res)
	case "aws_lb":
		spec, note, ok = mapLB(r, res)
	case "aws_vpn_gateway":
		spec, note, ok = mapVPNGateway(r, res)
	case "aws_fsx_lustre_filesystem":
		spec, note, ok = mapFSxLustre(r, res)
	default:
		if p, _, _ := strings.Cut(r.Type, "_"); p != "aws" {
			return KindUnsupported, nil, "unknown provider " + p + " (only aws is priced)"
		}
		return KindUnsupported, nil, "unsupported resource type (not priced)"
	}
	if !ok {
		return KindFixed, nil, note
	}
	if spec != nil {
		spec.Region = region
		for i := range spec.Rates {
			spec.Rates[i].Region = region
		}
	}
	return kind, spec, note
}

func mapEC2(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	it, ok := resStr(r, res, "instance_type")
	if !ok {
		return nil, "instance_type unresolved", false
	}
	tenancy := "Shared"
	if t, ok := resStr(r, res, "tenancy"); ok {
		tenancy = t
	}
	spec := ec2InstanceSpec(it, it, 1)
	for i, f := range spec.Filters {
		if f.Field == "tenancy" {
			spec.Filters[i] = tm("tenancy", tenancy)
		}
	}
	return spec, "", true
}

func mapRDS(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	ic, ok := resStr(r, res, "instance_class")
	if !ok {
		return nil, "instance_class unresolved", false
	}
	engine, hasEngine := resStr(r, res, "engine")
	if !hasEngine && lookupExpr(r, []string{"engine"}) != nil {
		return nil, "engine unresolved", false
	}
	deploy := "Single-AZ"
	if resBool(r, res, "multi_az") {
		deploy = "Multi-AZ"
	}
	return &Spec{
		ServiceCode: "AmazonRDS",
		Filters: []provider.Filter{
			tm("instanceType", ic),
			tm("databaseEngine", rdsEngine(engine)),
			tm("deploymentOption", deploy),
		},
		UsageQty: 730, Count: 1, Label: ic,
	}, "", true
}

func mapAuroraInstance(r *parser.Resource, res *resolver.Resolver, idx map[string]*parser.Resource) (*Spec, string, bool) {
	ic, ok := resStr(r, res, "instance_class")
	if !ok {
		return nil, "instance_class unresolved", false
	}
	engine, hasEngine := resStr(r, res, "engine")
	if !hasEngine {
		if lookupExpr(r, []string{"engine"}) != nil {
			return nil, "engine unresolved", false
		}
		if c := refResource(lookupExpr(r, []string{"cluster_identifier"}), idx); c != nil {
			engine, _ = resStr(c, res, "engine")
		}
	}
	return &Spec{
		ServiceCode: "AmazonRDS",
		Filters: []provider.Filter{
			tm("instanceType", ic),
			tm("databaseEngine", auroraEngine(engine)),
			tm("deploymentOption", "Single-AZ"),
		},
		UsageQty: 730, Count: 1, Label: ic,
	}, "", true
}

func mapAuroraCluster(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	st, _ := resStr(r, res, "storage_type")
	rate := func(base, label, unit, displayUnit string, mult float64) Rate {
		return Rate{
			Label: label, ServiceCode: "AmazonRDS",
			Filters:     []provider.Filter{tm("usagetype", base)},
			PreferUnit:  unit,
			DisplayMult: mult, DisplayUnit: displayUnit,
		}
	}
	var rates []Rate
	if st == "aurora-iopt1" {
		rates = []Rate{rate("Aurora:IO-OptimizedStorageUsage", "storage (I/O-Optimized)", "GB-Mo", "GB-mo", 0)}
	} else {
		rates = []Rate{
			rate("Aurora:StorageUsage", "storage", "GB-Mo", "GB-mo", 0),
			rate("Aurora:StorageIOUsage", "I/O", "IOs", "1M I/O", 1_000_000),
		}
	}
	return &Spec{Label: "Aurora storage & I/O", Note: "Aurora storage & I/O — billed by usage (instances are priced via aws_rds_cluster_instance)", Rates: rates}, "", true
}

func mapSecret(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	return &Spec{
		ServiceCode: "AWSSecretsManager",
		Filters: []provider.Filter{
			tm("usagetype", "AWSSecretsManager-Secrets"),
		},
		UsageQty: 1, Count: 1, Label: "Secret", PreferUnit: "Secrets",
	}, "", true
}

func mapDBProxy(r *parser.Resource, res *resolver.Resolver, idx map[string]*parser.Resource) (*Spec, string, bool) {
	filters := []provider.Filter{tm("usagetype", "RDS:ProxyUsage")}
	if vcpu := proxyTargetVCPU(r, res, idx); vcpu > 0 {
		return &Spec{
			ServiceCode: "AmazonRDS", Filters: filters,
			UsageQty: 730, Count: vcpu, PreferUnit: "Hrs",
			Label: fmt.Sprintf("RDS Proxy × %d vCPU", vcpu),
		}, "", true
	}
	return &Spec{Label: "RDS Proxy", Note: "RDS Proxy — target vCPU unresolved, monthly ≈ unit price × vCPU count × 730", Rates: []Rate{{
		Label: "vCPU", ServiceCode: "AmazonRDS",
		Filters:    filters,
		PreferUnit: "Hrs", DisplayUnit: "vCPU-hour",
	}}}, "", true
}

func proxyTargetVCPU(proxy *parser.Resource, res *resolver.Resolver, idx map[string]*parser.Resource) int {
	total := 0
	for _, t := range idx {
		if t.Type != "aws_db_proxy_target" {
			continue
		}
		if refResource(lookupExpr(t, []string{"db_proxy_name"}), idx) != proxy {
			continue
		}
		if inst := refResource(lookupExpr(t, []string{"db_instance_identifier"}), idx); inst != nil {
			if c, ok := resStr(inst, res, "instance_class"); ok {
				if v, ok := classVCPU(c); ok {
					total += v
				}
			}
			continue
		}
		if cl := refResource(lookupExpr(t, []string{"db_cluster_identifier"}), idx); cl != nil {
			for _, ci := range idx {
				if ci.Type != "aws_rds_cluster_instance" {
					continue
				}
				if refResource(lookupExpr(ci, []string{"cluster_identifier"}), idx) != cl {
					continue
				}
				if c, ok := resStr(ci, res, "instance_class"); ok {
					if v, ok := classVCPU(c); ok {
						total += v
					}
				}
			}
		}
	}
	return total
}

func classVCPU(class string) (int, bool) {
	parts := strings.Split(class, ".")
	if len(parts) < 3 {
		return 0, false
	}
	size := parts[len(parts)-1]
	if strings.HasPrefix(class, "t2.") || strings.HasPrefix(class, "db.t2.") {
		switch size {
		case "nano", "micro", "small":
			return 1, true
		case "medium", "large":
			return 2, true
		}
	}
	switch size {
	case "nano", "micro", "small", "medium", "large":
		return 2, true
	case "xlarge":
		return 4, true
	default:
		if n, err := strconv.Atoi(strings.TrimSuffix(size, "xlarge")); err == nil && n > 0 {
			return 4 * n, true
		}
	}
	return 0, false
}

func auroraEngine(e string) string {
	if strings.Contains(e, "postgres") {
		return "Aurora PostgreSQL"
	}
	return "Aurora MySQL"
}

// mapDBInstance prices DocumentDB and Neptune instance classes. Their
// Price List rows carry instanceType but no deploymentOption attribute
// (unlike RDS) — adding that filter matches nothing.
func mapDBInstance(r *parser.Resource, res *resolver.Resolver, serviceCode string) (*Spec, string, bool) {
	ic, ok := resStr(r, res, "instance_class")
	if !ok {
		return nil, "instance_class unresolved", false
	}
	return &Spec{
		ServiceCode: serviceCode,
		Filters:     []provider.Filter{tm("instanceType", ic)},
		UsageQty:    730, Count: 1, Label: ic,
	}, "", true
}

func mapRedshift(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	it, ok := resStr(r, res, "node_type")
	if !ok {
		return nil, "node_type unresolved", false
	}
	count := 1
	if n, ok := resNum(r, res, "number_of_nodes"); ok && n > 0 {
		count = int(n)
	}
	return &Spec{
		ServiceCode: "AmazonRedshift",
		Filters: []provider.Filter{
			tm("instanceType", it),
		},
		UsageQty: 730, Count: count, Label: fmt.Sprintf("%s × %d nodes", it, count),
	}, "", true
}

func mapOpenSearch(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	it, ok := resStr(r, res, "cluster_config.instance_type")
	if !ok {
		return nil, "cluster_config.instance_type unresolved", false
	}
	count := 1
	if n, ok := resNum(r, res, "cluster_config.instance_count"); ok && n > 0 {
		count = int(n)
	}
	return &Spec{
		ServiceCode: "AmazonOpenSearchService",
		Filters: []provider.Filter{
			tm("instanceType", it),
		},
		UsageQty: 730, Count: count, Label: fmt.Sprintf("%s × %d nodes", it, count),
	}, "", true
}

func mapMSK(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	it, ok := resStr(r, res, "broker_node_group_info.instance_type")
	if !ok {
		return nil, "broker instance_type unresolved", false
	}
	computeFamily := strings.TrimPrefix(it, "kafka.")
	count := 1
	if n, ok := resNum(r, res, "number_of_broker_nodes"); ok && n > 0 {
		count = int(n)
	}
	return &Spec{
		ServiceCode: "AmazonMSK",
		Filters: []provider.Filter{
			tm("computeFamily", computeFamily),
			tm("operation", "RunBroker"),
		},
		UsageQty: 730, Count: count, Label: fmt.Sprintf("%s × %d brokers", it, count),
	}, "", true
}

// flatHourly prices a resource that bills a flat hourly usagetype with no
// resource-specific attributes (730 h/mo). TGW attachments (and the gateway
// itself) bill under TransitGateway-Hours; EKS bills the control plane under
// AmazonEKS-Hours:perCluster (Auto Mode instance management and extended
// support bill separately and are not modeled); site-to-site VPN bills
// VPN-Usage-Hours:ipsec.1 per connection (data transfer billed separately).
func flatHourly(serviceCode, usagetype, label string) *Spec {
	return &Spec{
		ServiceCode: serviceCode,
		Filters:     []provider.Filter{tm("usagetype", usagetype)},
		UsageQty:    730, Count: 1, Label: label,
	}
}

func mapElastiCache(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	nt, ok := resStr(r, res, "node_type")
	if !ok {
		return nil, "node_type unresolved", false
	}
	engine := "Redis"
	if e, ok := resStr(r, res, "engine"); ok {
		engine = elasticacheEngine(e)
	}
	count := 1
	// Cluster mode: nodes = num_node_groups × (replicas_per_node_group + 1).
	// Both attrs are null outside cluster mode, falling through to
	// num_cache_clusters (non-cluster replication group / cache cluster).
	for _, k := range []string{"num_node_groups", "number_of_node_groups"} {
		if n, ok := resNum(r, res, k); ok && n > 0 {
			replicas := 0
			if rp, ok := resNum(r, res, "replicas_per_node_group"); ok && rp >= 0 {
				replicas = int(rp)
			}
			count = int(n) * (replicas + 1)
			break
		}
	}
	if count == 1 {
		for _, k := range []string{"num_cache_clusters", "number_of_cache_clusters"} {
			if n, ok := resNum(r, res, k); ok && n > 0 {
				count = int(n)
				break
			}
		}
	}
	return &Spec{
		ServiceCode: "AmazonElastiCache",
		Filters: []provider.Filter{
			tm("instanceType", nt),
			tm("cacheEngine", engine),
		},
		UsageQty: 730, Count: count, Label: fmt.Sprintf("%s × %d nodes", nt, count),
	}, "", true
}

func mapEBS(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	size, ok := resNum(r, res, "size")
	if !ok {
		return nil, "size unresolved", false
	}
	vtype := "gp3"
	if t, ok := resStr(r, res, "type"); ok {
		vtype = t
	}
	return &Spec{
		ServiceCode: "AmazonEC2",
		Filters: []provider.Filter{
			tm("volumeApiName", vtype),
		},
		UsageQty: size, Count: 1, Label: fmt.Sprintf("%s %gGB", vtype, size),
		PreferUnit: "GB-Mo",
	}, "", true
}

func mapASG(r *parser.Resource, res *resolver.Resolver, idx map[string]*parser.Resource) (*Spec, string, bool) {
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
		if lt == nil {
			lt = resolveResourceRef(src, res, idx)
		}
	}
	if lt == nil {
		return nil, "ASG launch_template/launch_configuration reference unresolved", false
	}
	it, ok := resStr(lt, res, "instance_type")
	if !ok {
		return nil, "launch_template instance_type unresolved", false
	}
	if count == 0 {
		return nil, "ASG capacity (desired/min) unresolved", false
	}
	return ec2InstanceSpec(it, fmt.Sprintf("%s × %d (ASG)", it, count), count), "", true
}

func mapEKSNodeGroup(r *parser.Resource, res *resolver.Resolver, idx map[string]*parser.Resource) (*Spec, string, bool) {
	count := 0
	if n, ok := resNum(r, res, "scaling_config.desired_size"); ok && n > 0 {
		count = int(n)
	}
	var it string
	if lt := lookupExpr(r, []string{"launch_template.id", "launch_template.name", "launch_template_name"}); lt != nil {
		ltRes := refResource(lt, idx)
		if ltRes == nil {
			ltRes = resolveResourceRef(lt, res, idx)
		}
		if ltRes != nil {
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
	multiType := ""
	if e, ok := r.Exprs["instance_types"]; ok {
		if v, ok := res.ResolveExpr(e); ok && v.IsKnown() && !v.IsNull() {
			t := v.Type()
			if t.IsListType() || t.IsSetType() || t.IsTupleType() {
				if n := v.LengthInt(); n > 1 {
					multiType = fmt.Sprintf(" (first of %d types)", n)
				}
			}
		}
	}
	if it == "" {
		return nil, "EKS node group instance_type unresolved (launch_template/instance_types)", false
	}
	if count == 0 {
		return nil, "EKS node group desired_size unresolved", false
	}
	return ec2InstanceSpec(it, fmt.Sprintf("%s × %d (EKS node)%s", it, count, multiType), count), "", true
}

func mapNATGateway(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	return &Spec{
		ServiceCode: "AmazonEC2",
		Filters: []provider.Filter{
			tm("usagetype", "NatGateway-Hours"),
		},
		UsageQty: 730, Count: 1, Label: "NAT Gateway", PreferUnit: "Hrs",
	}, "", true
}

func mapVPNGateway(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	return &Spec{
		ServiceCode: "AmazonVPC",
		Filters: []provider.Filter{
			tm("usagetype", "VPN-Usage-Hours:ipsec.1"),
		},
		UsageQty: 730, Count: 1, Label: "VPN Gateway (billed as VPN connection)",
	}, "", true
}

func mapLB(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	lbt, ok := resStr(r, res, "load_balancer_type")
	if !ok || lbt == "" {
		return nil, "load_balancer_type unresolved", false
	}
	lbt = strings.ToUpper(lbt[:1]) + lbt[1:]
	// Pricing rows key on usagetype + productFamily, not loadBalancerType:
	// ALB and NLB hourly rows share usagetype LoadBalancerUsage.
	switch lbt {
	case "Application", "Network":
		return &Spec{
			ServiceCode: "AmazonEC2",
			Filters: []provider.Filter{
				tm("usagetype", "LoadBalancerUsage"),
				tm("productFamily", "Load Balancer-"+lbt),
			},
			UsageQty: 730, Count: 1, Label: lbt + " LB", PreferUnit: "Hrs",
		}, "", true
	default:
		// Gateway LB: billed per GWLB endpoint in the spoke VPCs, not hourly.
		return nil, lbt + " LB — billed per GWLB endpoint (usage-based)", false
	}
}

func mapFSxLustre(r *parser.Resource, res *resolver.Resolver) (*Spec, string, bool) {
	size, ok := resNum(r, res, "storage_capacity")
	if !ok {
		return nil, "storage_capacity unresolved", false
	}
	return &Spec{
		ServiceCode: "AmazonFSx",
		Filters: []provider.Filter{
			tm("fileSystemType", "LUSTRE"),
			tm("usagetype", "FSxLustre-Storage-GB-Mo"),
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

// resolveResourceRef finds the resource an expression ultimately references,
// following unresolved locals transitively. This handles the pattern
//
//	launch_template { id = local.lt_block.id }
//	locals { lt_block = { id = one(aws_launch_template.default[*].id) } }
//
// where the id VALUE is computed but the TARGET resource is discoverable.
func resolveResourceRef(expr hcl.Expression, res *resolver.Resolver, idx map[string]*parser.Resource) *parser.Resource {
	return chaseResourceRef(expr, res, idx, map[string]bool{})
}

func chaseResourceRef(expr hcl.Expression, res *resolver.Resolver, idx map[string]*parser.Resource, visited map[string]bool) *parser.Resource {
	for _, t := range expr.Variables() {
		if len(t) < 2 {
			continue
		}
		root, ok := t[0].(hcl.TraverseRoot)
		if !ok {
			continue
		}
		attr, ok := t[1].(hcl.TraverseAttr)
		if !ok {
			continue
		}
		addr := root.Name + "." + attr.Name
		if r, ok := idx[addr]; ok {
			return r
		}
		if root.Name == "local" && !visited[attr.Name] {
			visited[attr.Name] = true
			if la, ok := res.LocalExpr(attr.Name); ok {
				if r := chaseResourceRef(la, res, idx, visited); r != nil {
					return r
				}
			}
		}
	}
	return nil
}
