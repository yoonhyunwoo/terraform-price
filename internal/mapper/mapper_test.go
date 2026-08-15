package mapper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoonhyunwoo/terraform-price/internal/tf/parser"
	"github.com/yoonhyunwoo/terraform-price/internal/tf/resolver"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func idxOf(rs []*parser.Resource) map[string]*parser.Resource {
	m := make(map[string]*parser.Resource, len(rs))
	for _, r := range rs {
		m[r.Type+"."+r.Name] = r
	}
	return m
}

func filterVal(spec *Spec, field string) string {
	for _, f := range spec.Filters {
		if f.Field == field {
			return f.Value
		}
	}
	return ""
}

func TestMapASGResolvesLaunchTemplate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "asg.tf", `
resource "aws_launch_template" "web" {
  instance_type = "t3.micro"
}
resource "aws_autoscaling_group" "web" {
  desired_capacity = 3
  launch_template { id = aws_launch_template.web.id }
}
`)
	rs, err := parser.ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := resolver.NewResolver(dir)
	var asg *parser.Resource
	for _, r := range rs {
		if r.Type == "aws_autoscaling_group" {
			asg = r
		}
	}
	if asg == nil {
		t.Fatal("ASG not parsed")
	}
	kind, spec, note := MapResource(asg, res, idxOf(rs), "ap-northeast-2")
	if kind != KindFixed || spec == nil {
		t.Fatalf("expected Fixed spec, got kind=%v spec=%v note=%q", kind, spec, note)
	}
	if spec.Count != 3 {
		t.Errorf("count: want 3, got %d", spec.Count)
	}
	if got := filterVal(spec, "instanceType"); got != "t3.micro" {
		t.Errorf("instanceType filter: want t3.micro, got %q", got)
	}
}

func TestMapEKSNodeGroupInstanceTypesList(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "eks.tf", `
resource "aws_eks_node_group" "ng" {
  instance_types = ["m5.large"]
  scaling_config {
    desired_size = 2
  }
}
`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	var ng *parser.Resource
	for _, r := range rs {
		if r.Type == "aws_eks_node_group" {
			ng = r
		}
	}
	kind, spec, note := MapResource(ng, res, idxOf(rs), "ap-northeast-2")
	if kind != KindFixed || spec == nil {
		t.Fatalf("expected Fixed spec, got kind=%v spec=%v note=%q", kind, spec, note)
	}
	if spec.Count != 2 {
		t.Errorf("count: want 2, got %d", spec.Count)
	}
	if got := filterVal(spec, "instanceType"); got != "m5.large" {
		t.Errorf("instanceType filter: want m5.large, got %q", got)
	}
}

func TestMapRedshiftNodeTypeAndCount(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "redshift.tf", `
resource "aws_redshift_cluster" "c" {
  node_type       = "ra3.xlplus"
  number_of_nodes = 3
}
`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	var rc *parser.Resource
	for _, r := range rs {
		if r.Type == "aws_redshift_cluster" {
			rc = r
		}
	}
	kind, spec, note := MapResource(rc, res, idxOf(rs), "ap-northeast-2")
	if kind != KindFixed || spec == nil {
		t.Fatalf("expected Fixed spec, got kind=%v spec=%v note=%q", kind, spec, note)
	}
	if spec.Count != 3 {
		t.Errorf("count: want 3, got %d", spec.Count)
	}
	if filterVal(spec, "instanceType") != "ra3.xlplus" {
		t.Errorf("instanceType filter: want ra3.xlplus, got %q", filterVal(spec, "instanceType"))
	}
}

func TestSpecCarriesRegionAndNoLocationFilter(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.tf", `
resource "aws_instance" "web" { instance_type = "t3.micro" }
resource "aws_rds_cluster" "aurora" { }`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	for _, r := range rs {
		_, spec, _ := MapResource(r, res, idxOf(rs), "ap-northeast-2")
		if spec == nil {
			t.Fatalf("%s: no spec", r.Type)
		}
		if spec.Region != "ap-northeast-2" {
			t.Errorf("%s: spec.Region = %q, want ap-northeast-2", r.Type, spec.Region)
		}
		for _, rt := range spec.Rates {
			if rt.Region != "ap-northeast-2" {
				t.Errorf("%s: rate %q Region = %q", r.Type, rt.Label, rt.Region)
			}
		}
		for _, f := range spec.Filters {
			if f.Field == "location" {
				t.Errorf("%s: location filter must not live in mapper filters (awsprice composes it)", r.Type)
			}
			if f.Field == "usagetype" && strings.Contains(f.Value, "-") && !strings.Contains(f.Value, ":") {
				t.Errorf("%s: usagetype filter %q looks region-prefixed; mapper must pass the neutral base only", r.Type, f.Value)
			}
		}
	}
}

func TestMapNATGatewayUsagetype(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "nat.tf", `resource "aws_nat_gateway" "n" {}`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	kind, spec, note := MapResource(rs[0], res, idxOf(rs), "ap-northeast-2")
	if kind != KindFixed || spec == nil {
		t.Fatalf("got kind=%v spec=%v note=%q", kind, spec, note)
	}
	if got := filterVal(spec, "usagetype"); got != "NatGateway-Hours" {
		t.Errorf("usagetype filter: want neutral base NatGateway-Hours (awsprice adds the region prefix), got %q", got)
	}
	if spec.PreferUnit != "Hrs" {
		t.Errorf("PreferUnit: want Hrs, got %q", spec.PreferUnit)
	}
}

func TestMapLBType(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "lb.tf", `resource "aws_lb" "l" { load_balancer_type = "network" }`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	kind, spec, note := MapResource(rs[0], res, idxOf(rs), "ap-northeast-2")
	if kind != KindFixed || spec == nil {
		t.Fatalf("got kind=%v spec=%v note=%q", kind, spec, note)
	}
	if got := filterVal(spec, "usagetype"); got != "LoadBalancerUsage" {
		t.Errorf("usagetype filter: want LoadBalancerUsage, got %q", got)
	}
	if got := filterVal(spec, "productFamily"); got != "Load Balancer-Network" {
		t.Errorf("productFamily filter: want Load Balancer-Network, got %q", got)
	}
}

func TestMapFSxLustre(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fsx.tf", `resource "aws_fsx_lustre_filesystem" "f" { storage_capacity = 1200 }`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	kind, spec, note := MapResource(rs[0], res, idxOf(rs), "ap-northeast-2")
	if kind != KindFixed || spec == nil {
		t.Fatalf("got kind=%v spec=%v note=%q", kind, spec, note)
	}
	if spec.UsageQty != 1200 {
		t.Errorf("UsageQty: want 1200, got %g", spec.UsageQty)
	}
	if filterVal(spec, "usagetype") != "FSxLustre-Storage-GB-Mo" {
		t.Errorf("usagetype: want neutral base FSxLustre-Storage-GB-Mo, got %q", filterVal(spec, "usagetype"))
	}
}

func TestVariableAndUnsupportedRouting(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "misc.tf", `
resource "aws_s3_bucket" "b" {}
resource "aws_widget_widget" "w" {}
resource "aws_kms_key" "k" {}
`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	for _, r := range rs {
		kind, spec, note := MapResource(r, res, idxOf(rs), "ap-northeast-2")
		switch r.Type {
		case "aws_s3_bucket":
			if kind != KindVariable || spec != nil {
				t.Errorf("s3: want Variable/nil-spec, got kind=%v spec=%v", kind, spec)
			}
		case "aws_widget_widget":
			if kind != KindUnsupported || note == "" {
				t.Errorf("unknown type: want Unsupported w/ note, got kind=%v note=%q", kind, note)
			}
		case "aws_kms_key":
			if kind != KindFixed {
				t.Errorf("kms: want Fixed, got kind=%v", kind)
			}
		}
	}
}

func TestClassVCPU(t *testing.T) {
	cases := map[string]int{
		"db.t3.micro": 2, "db.t3.medium": 2, "db.r6i.large": 2,
		"db.r8g.xlarge": 4, "db.r5.2xlarge": 8, "db.r6g.4xlarge": 16,
		"db.m5.24xlarge": 96,
		"db.t2.micro":    1, "db.t2.small": 1, "db.t2.medium": 2,
		"db.t2.xlarge": 4, "db.t2.2xlarge": 8,
	}
	for class, want := range cases {
		got, ok := classVCPU(class)
		if !ok || got != want {
			t.Errorf("classVCPU(%q): want %d, got %d ok=%v", class, want, got, ok)
		}
	}
	if _, ok := classVCPU("garbage"); ok {
		t.Errorf("garbage class should not resolve")
	}
}

func TestMapProxyResolvesTargetVCPU(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "proxy.tf", `
resource "aws_db_instance" "rds" { instance_class = "db.r5.2xlarge" }
resource "aws_db_proxy" "p" { name = "p" }
resource "aws_db_proxy_target" "t" {
  db_proxy_name          = aws_db_proxy.p.name
  db_instance_identifier = aws_db_instance.rds.identifier
}
`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	var proxy *parser.Resource
	for _, r := range rs {
		if r.Type == "aws_db_proxy" {
			proxy = r
		}
	}
	kind, spec, note := MapResource(proxy, res, idxOf(rs), "ap-northeast-2")
	if kind != KindFixed || spec == nil {
		t.Fatalf("want Fixed spec, got kind=%v spec=%v note=%q", kind, spec, note)
	}
	if spec.Count != 8 {
		t.Errorf("vCPU count: want 8 (r5.2xlarge), got %d", spec.Count)
	}
}

func TestMapFreeClassification(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "free.tf", `
resource "aws_iam_role" "r" {}
resource "aws_security_group" "s" {}
resource "aws_vpc" "v" {}
`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	for _, r := range rs {
		kind, spec, note := MapResource(r, res, idxOf(rs), "ap-northeast-2")
		if kind != KindFree || spec != nil {
			t.Errorf("%s: want KindFree/nil-spec, got kind=%v spec=%v note=%q", r.Type, kind, spec, note)
		}
	}
}

func TestModuleBlockSurfacedAsUnsupported(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "m.tf", `module "db" { source = "./modules/rds" }`)
	rs, err := parser.ParseDir(dir)
	if err != nil || len(rs) != 1 {
		t.Fatalf("parse: %v %+v", err, rs)
	}
	kind, spec, note := MapResource(rs[0], resolver.NewResolver(dir), idxOf(rs), "ap-northeast-2")
	if kind != KindUnsupported || spec != nil || !strings.Contains(note, "module") {
		t.Fatalf("module: want Unsupported/nil-spec/module note, got kind=%v spec=%v note=%q", kind, spec, note)
	}
}

func TestMapRDSEngineUnresolvedFails(t *testing.T) {
	dir := t.TempDir()
	// No default for var.engine: conditional on an unset var stays unresolved.
	writeFile(t, dir, "rds.tf", `
variable "engine" { type = string }
resource "aws_db_instance" "db" {
  instance_class = "db.t3.micro"
  engine         = var.engine == "x" ? "mysql" : "postgres"
}`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	kind, spec, note := MapResource(rs[0], res, idxOf(rs), "ap-northeast-2")
	if kind != KindFixed || spec != nil || note != "engine unresolved" {
		t.Fatalf("want Fixed/nil/'engine unresolved', got kind=%v spec=%v note=%q", kind, spec, note)
	}
}

func TestMapRDSEngineFromVarDefault(t *testing.T) {
	dir := t.TempDir()
	// Variable defaults now resolve: engine = "postgres" via default.
	writeFile(t, dir, "rds.tf", `
variable "engine" { default = "postgres" }
resource "aws_db_instance" "db" {
  instance_class = "db.t3.micro"
  engine         = var.engine
}`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	kind, spec, _ := MapResource(rs[0], res, idxOf(rs), "ap-northeast-2")
	if kind != KindFixed || spec == nil {
		t.Fatalf("want Fixed/spec with default-resolved engine, got kind=%v spec=%v", kind, spec)
	}
}

func TestMapRDSEngineAbsentDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rds.tf", `resource "aws_db_instance" "db" { instance_class = "db.t3.micro" }`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	kind, spec, note := MapResource(rs[0], res, idxOf(rs), "ap-northeast-2")
	if kind != KindFixed || spec == nil || note != "" {
		t.Fatalf("absent engine should keep MySQL default, got kind=%v spec=%v note=%q", kind, spec, note)
	}
	if got := filterVal(spec, "databaseEngine"); got != "MySQL" {
		t.Fatalf("default engine filter: want MySQL, got %q", got)
	}
}

func TestMapAuroraClusterNoteConvention(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "aurora.tf", `resource "aws_rds_cluster" "c" { }`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	kind, spec, note := MapResource(rs[0], res, idxOf(rs), "ap-northeast-2")
	if kind != KindVariable || spec == nil {
		t.Fatalf("want Variable/spec, got kind=%v spec=%v", kind, spec)
	}
	if note != "" {
		t.Fatalf("ok=true must return empty note (display text belongs to Spec.Note), got %q", note)
	}
	if spec.Note == "" {
		t.Fatal("display note should live in Spec.Note")
	}
}

func TestMapEKSNodeGroupMultiTypeSuffix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "eks.tf", `
resource "aws_eks_node_group" "ng" {
  instance_types = ["t3.medium", "t3.large", "t3.xlarge"]
  scaling_config { desired_size = 2 }
}`)
	rs, _ := parser.ParseDir(dir)
	res := resolver.NewResolver(dir)
	kind, spec, _ := MapResource(rs[0], res, idxOf(rs), "ap-northeast-2")
	if kind != KindFixed || spec == nil {
		t.Fatalf("want Fixed spec, got kind=%v spec=%v", kind, spec)
	}
	if !strings.Contains(spec.Label, "first of 3 types") {
		t.Fatalf("multi-type approximation must be surfaced, got label %q", spec.Label)
	}
}

// Regression: null or scalar instance_types used to panic at LengthInt.
func TestEKSInstanceTypesNullNoPanic(t *testing.T) {
	for name, body := range map[string]string{
		"null":   `instance_types = null`,
		"scalar": `instance_types = "t3.medium"`,
	} {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("%s: panic: %v", name, p)
				}
			}()
			dir := t.TempDir()
			writeFile(t, dir, "eks.tf", "resource \"aws_eks_node_group\" \"ng\" {\n  "+body+"\n  scaling_config { desired_size = 2 }\n}")
			rs, _ := parser.ParseDir(dir)
			kind, spec, note := MapResource(rs[0], resolver.NewResolver(dir), idxOf(rs), "ap-northeast-2")
			if kind != KindFixed || spec != nil || note == "" {
				t.Errorf("%s: want unresolved row, got kind=%v spec=%v note=%q", name, kind, spec, note)
			}
		}()
	}
}

// DocumentDB and Neptune price rows carry no deploymentOption attribute;
// mapDBInstance must not filter on it (corpus: terragoat neptune matched
// nothing until the filter was removed).
func TestMapDBInstanceNoDeploymentOption(t *testing.T) {
	for _, typ := range []string{"aws_docdb_cluster_instance", "aws_neptune_cluster_instance"} {
		dir := t.TempDir()
		writeFile(t, dir, "db.tf", "resource \""+typ+"\" \"i\" {\n  instance_class = \"db.t3.medium\"\n}")
		rs, _ := parser.ParseDir(dir)
		_, spec, note := MapResource(rs[0], resolver.NewResolver(dir), idxOf(rs), "ap-northeast-2")
		if spec == nil {
			t.Fatalf("%s: %v", typ, note)
		}
		for _, f := range spec.Filters {
			if f.Field == "deploymentOption" {
				t.Errorf("%s: deploymentOption filter present, matches nothing in the price list", typ)
			}
		}
	}
}

// EKS clusters bill $0.60/cluster-hr once their Kubernetes version passes the
// end of standard support and the upgrade policy is EXTENDED (the default);
// STANDARD policy auto-upgrades before extended support starts.
func TestMapEKSClusterExtendedSupport(t *testing.T) {
	orig := eksNow
	t.Cleanup(func() { eksNow = orig })
	eksNow = func() time.Time { return time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC) }

	cases := []struct {
		name      string
		body      string
		wantFlat  bool
		wantLabel string
	}{
		{"unset version", "", false, "EKS control plane"},
		{"standard version", "version = \"1.34\"", false, "EKS control plane"},
		{"unknown version", "version = \"1.99\"", false, "EKS control plane"},
		{"extended", "version = \"1.31\"", true, "extended support"},
		{"past extended EOL", "version = \"1.23\"", true, "past extended EOL"},
		{"standard policy", "version = \"1.31\"\n  upgrade_policy { support_type = \"STANDARD\" }", false, "EKS control plane"},
	}
	for _, tc := range cases {
		dir := t.TempDir()
		writeFile(t, dir, "eks.tf", "resource \"aws_eks_cluster\" \"c\" {\n"+tc.body+"\n}")
		rs, _ := parser.ParseDir(dir)
		if len(rs) == 0 {
			t.Fatalf("%s: parse failed", tc.name)
		}
		_, spec, note := MapResource(rs[0], resolver.NewResolver(dir), idxOf(rs), "us-east-1")
		if spec == nil {
			t.Fatalf("%s: %v", tc.name, note)
		}
		if tc.wantFlat && spec.FlatPrice == nil {
			t.Errorf("%s: want flat extended price, got nil (label %q)", tc.name, spec.Label)
		}
		if !tc.wantFlat && spec.FlatPrice != nil {
			t.Errorf("%s: want API price, got flat %v", tc.name, *spec.FlatPrice)
		}
		if !strings.Contains(spec.Label, tc.wantLabel) {
			t.Errorf("%s: label %q missing %q", tc.name, spec.Label, tc.wantLabel)
		}
	}
}

// The bare instanceType filter matches CPUCredits (t3) and IO-Optimized rows
// too; the usagetype filter pins the standard InstanceUsage rate (live probe:
// db.t3.medium DocDB rows 0.078 Hrs / 0.0858 IO-Opt / 0.09 vCPU-Hours).
func TestMapDBInstancePinsInstanceUsage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "db.tf", "resource \"aws_docdb_cluster_instance\" \"i\" {\n  instance_class = \"db.t3.medium\"\n}")
	rs, _ := parser.ParseDir(dir)
	_, spec, note := MapResource(rs[0], resolver.NewResolver(dir), idxOf(rs), "us-east-1")
	if spec == nil {
		t.Fatal(note)
	}
	if got := filterVal(spec, "usagetype"); got != "InstanceUsage:db.t3.medium" {
		t.Fatalf("usagetype filter = %q, want InstanceUsage:db.t3.medium", got)
	}
}
