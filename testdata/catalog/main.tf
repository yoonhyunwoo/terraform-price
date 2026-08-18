# Synthetic Terraform driving the embedded price catalog. `make catalog` runs
# terraform-price over this file once per region in CATALOG_REGIONS and
# collects every price it looks up into internal/catalog/prices.json.
# To extend coverage, add a resource here and re-run `make catalog`.
terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
}

# --- EC2 instances: common families/sizes ---
resource "aws_instance" "t3_nano" {
  instance_type = "t3.nano"
}
resource "aws_instance" "t3_micro" {
  instance_type = "t3.micro"
}
resource "aws_instance" "t3_small" {
  instance_type = "t3.small"
}
resource "aws_instance" "t3_medium" {
  instance_type = "t3.medium"
}
resource "aws_instance" "t3_large" {
  instance_type = "t3.large"
}
resource "aws_instance" "t3_xlarge" {
  instance_type = "t3.xlarge"
}
resource "aws_instance" "t3_2xlarge" {
  instance_type = "t3.2xlarge"
}
resource "aws_instance" "t3a_micro" {
  instance_type = "t3a.micro"
}
resource "aws_instance" "t3a_medium" {
  instance_type = "t3a.medium"
}
resource "aws_instance" "t3a_large" {
  instance_type = "t3a.large"
}
resource "aws_instance" "t3a_xlarge" {
  instance_type = "t3a.xlarge"
}
resource "aws_instance" "t2_micro" {
  instance_type = "t2.micro"
}
resource "aws_instance" "t2_small" {
  instance_type = "t2.small"
}
resource "aws_instance" "t2_medium" {
  instance_type = "t2.medium"
}
resource "aws_instance" "m5_large" {
  instance_type = "m5.large"
}
resource "aws_instance" "m5_xlarge" {
  instance_type = "m5.xlarge"
}
resource "aws_instance" "m5_2xlarge" {
  instance_type = "m5.2xlarge"
}
resource "aws_instance" "m5_4xlarge" {
  instance_type = "m5.4xlarge"
}
resource "aws_instance" "m5a_large" {
  instance_type = "m5a.large"
}
resource "aws_instance" "m5a_xlarge" {
  instance_type = "m5a.xlarge"
}
resource "aws_instance" "m5a_2xlarge" {
  instance_type = "m5a.2xlarge"
}
resource "aws_instance" "m6i_large" {
  instance_type = "m6i.large"
}
resource "aws_instance" "m6i_xlarge" {
  instance_type = "m6i.xlarge"
}
resource "aws_instance" "m6i_2xlarge" {
  instance_type = "m6i.2xlarge"
}
resource "aws_instance" "m7g_large" {
  instance_type = "m7g.large"
}
resource "aws_instance" "m7g_xlarge" {
  instance_type = "m7g.xlarge"
}
resource "aws_instance" "c5_large" {
  instance_type = "c5.large"
}
resource "aws_instance" "c5_xlarge" {
  instance_type = "c5.xlarge"
}
resource "aws_instance" "c5_2xlarge" {
  instance_type = "c5.2xlarge"
}
resource "aws_instance" "c5_4xlarge" {
  instance_type = "c5.4xlarge"
}
resource "aws_instance" "c6i_large" {
  instance_type = "c6i.large"
}
resource "aws_instance" "c6i_xlarge" {
  instance_type = "c6i.xlarge"
}
resource "aws_instance" "c7g_large" {
  instance_type = "c7g.large"
}
resource "aws_instance" "c7g_xlarge" {
  instance_type = "c7g.xlarge"
}
resource "aws_instance" "r5_large" {
  instance_type = "r5.large"
}
resource "aws_instance" "r5_xlarge" {
  instance_type = "r5.xlarge"
}
resource "aws_instance" "r5_2xlarge" {
  instance_type = "r5.2xlarge"
}
resource "aws_instance" "r5_4xlarge" {
  instance_type = "r5.4xlarge"
}
resource "aws_instance" "r6i_large" {
  instance_type = "r6i.large"
}
resource "aws_instance" "r6i_xlarge" {
  instance_type = "r6i.xlarge"
}
resource "aws_instance" "r7g_large" {
  instance_type = "r7g.large"
}
resource "aws_instance" "g5_xlarge" {
  instance_type = "g5.xlarge"
}
resource "aws_instance" "g4dn_xlarge" {
  instance_type = "g4dn.xlarge"
}

# --- EBS volumes: gp2/gp3/st1/io1 per GB-mo (size-independent rate) ---
resource "aws_ebs_volume" "gp3" {
  type = "gp3"
  size = 100
}
resource "aws_ebs_volume" "gp2" {
  type = "gp2"
  size = 100
}
resource "aws_ebs_volume" "st1" {
  type = "st1"
  size = 500
}
resource "aws_ebs_volume" "io1" {
  type = "io1"
  size = 100
}

# --- Networking flat rates ---
resource "aws_nat_gateway" "this" {}
resource "aws_vpn_connection" "this" {
  type = "ipsec.1"
}
resource "aws_ec2_transit_gateway_vpc_attachment" "this" {}
resource "aws_lb" "nlb" {
  load_balancer_type = "network"
}

# --- RDS: MySQL/PostgreSQL common classes, Single/Multi-AZ ---
resource "aws_db_instance" "mysql_t3_micro" {
  instance_class = "db.t3.micro"
  engine = "mysql"
  allocated_storage = 20
  storage_type = "gp3"
}
resource "aws_db_instance" "mysql_t3_small" {
  instance_class = "db.t3.small"
  engine = "mysql"
  allocated_storage = 20
  storage_type = "gp3"
}
resource "aws_db_instance" "mysql_t3_medium" {
  instance_class = "db.t3.medium"
  engine = "mysql"
  allocated_storage = 50
  storage_type = "gp3"
}
resource "aws_db_instance" "mysql_t3_large" {
  instance_class = "db.t3.large"
  engine = "mysql"
  allocated_storage = 50
  storage_type = "gp3"
}
resource "aws_db_instance" "mysql_t3_xlarge" {
  instance_class = "db.t3.xlarge"
  engine = "mysql"
  allocated_storage = 100
  storage_type = "gp3"
}
resource "aws_db_instance" "mysql_m5_large" {
  instance_class = "db.m5.large"
  engine = "mysql"
  allocated_storage = 100
  storage_type = "gp3"
}
resource "aws_db_instance" "mysql_m5_xlarge" {
  instance_class = "db.m5.xlarge"
  engine = "mysql"
  allocated_storage = 100
  storage_type = "gp3"
}
resource "aws_db_instance" "mysql_m5_2xlarge" {
  instance_class = "db.m5.2xlarge"
  engine = "mysql"
  allocated_storage = 200
  storage_type = "gp3"
  multi_az = true
}
resource "aws_db_instance" "mysql_m5_4xlarge" {
  instance_class = "db.m5.4xlarge"
  engine = "mysql"
  allocated_storage = 200
  storage_type = "gp3"
  multi_az = true
}
resource "aws_db_instance" "mysql_r5_large" {
  instance_class = "db.r5.large"
  engine = "mysql"
  allocated_storage = 100
  storage_type = "gp3"
}
resource "aws_db_instance" "mysql_r5_xlarge" {
  instance_class = "db.r5.xlarge"
  engine = "mysql"
  allocated_storage = 200
  storage_type = "gp3"
}
resource "aws_db_instance" "mysql_gp2" {
  instance_class = "db.t3.medium"
  engine = "mysql"
  allocated_storage = 50
  storage_type = "gp2"
}
resource "aws_db_instance" "pg_t3_micro" {
  instance_class = "db.t3.micro"
  engine = "postgres"
  allocated_storage = 20
  storage_type = "gp3"
}
resource "aws_db_instance" "pg_t3_medium" {
  instance_class = "db.t3.medium"
  engine = "postgres"
  allocated_storage = 50
  storage_type = "gp3"
}
resource "aws_db_instance" "pg_t3_large" {
  instance_class = "db.t3.large"
  engine = "postgres"
  allocated_storage = 50
  storage_type = "gp3"
}
resource "aws_db_instance" "pg_m5_large" {
  instance_class = "db.m5.large"
  engine = "postgres"
  allocated_storage = 100
  storage_type = "gp3"
}
resource "aws_db_instance" "pg_m5_xlarge" {
  instance_class = "db.m5.xlarge"
  engine = "postgres"
  allocated_storage = 100
  storage_type = "gp3"
}
resource "aws_db_instance" "pg_m5_2xlarge" {
  instance_class = "db.m5.2xlarge"
  engine = "postgres"
  allocated_storage = 200
  storage_type = "gp3"
  multi_az = true
}
resource "aws_db_instance" "pg_m6i_large" {
  instance_class = "db.m6i.large"
  engine = "postgres"
  allocated_storage = 100
  storage_type = "gp3"
}
resource "aws_db_instance" "pg_r5_large" {
  instance_class = "db.r5.large"
  engine = "postgres"
  allocated_storage = 100
  storage_type = "gp3"
}
resource "aws_db_instance" "pg_r6g_xlarge" {
  instance_class = "db.r6g.xlarge"
  engine = "postgres"
  allocated_storage = 200
  storage_type = "gp3"
}

# --- Aurora: storage & I/O usage rates + common cluster instances ---
resource "aws_rds_cluster" "aurora" {}
resource "aws_rds_cluster_instance" "aurora_mysql_t3_medium" {
  instance_class = "db.t3.medium"
  engine = "aurora-mysql"
}
resource "aws_rds_cluster_instance" "aurora_mysql_r5_large" {
  instance_class = "db.r5.large"
  engine = "aurora-mysql"
}
resource "aws_rds_cluster_instance" "aurora_pg_t3_medium" {
  instance_class = "db.t3.medium"
  engine = "aurora-postgresql"
}
resource "aws_rds_cluster_instance" "aurora_pg_r6g_large" {
  instance_class = "db.r6g.large"
  engine = "aurora-postgresql"
}

# --- ElastiCache: Redis/Memcached common node types ---
resource "aws_elasticache_cluster" "redis_t3_small" {
  engine = "redis"
  node_type = "cache.t3.small"
  num_cache_nodes = 1
}
resource "aws_elasticache_cluster" "redis_t3_medium" {
  engine = "redis"
  node_type = "cache.t3.medium"
  num_cache_nodes = 1
}
resource "aws_elasticache_cluster" "redis_t3_large" {
  engine = "redis"
  node_type = "cache.t3.large"
  num_cache_nodes = 1
}
resource "aws_elasticache_cluster" "redis_m5_large" {
  engine = "redis"
  node_type = "cache.m5.large"
  num_cache_nodes = 1
}
resource "aws_elasticache_cluster" "redis_m6g_large" {
  engine = "redis"
  node_type = "cache.m6g.large"
  num_cache_nodes = 1
}
resource "aws_elasticache_cluster" "redis_r6g_large" {
  engine = "redis"
  node_type = "cache.r6g.large"
  num_cache_nodes = 1
}
resource "aws_elasticache_cluster" "memcached_m5_large" {
  engine = "memcached"
  node_type = "cache.m5.large"
  num_cache_nodes = 1
}
resource "aws_elasticache_cluster" "memcached_m6g_large" {
  engine = "memcached"
  node_type = "cache.m6g.large"
  num_cache_nodes = 1
}

# --- DocumentDB / Neptune ---
resource "aws_docdb_cluster_instance" "docdb_t3_medium" {
  instance_class = "db.t3.medium"
}
resource "aws_docdb_cluster_instance" "docdb_r5_large" {
  instance_class = "db.r5.large"
}
resource "aws_docdb_cluster_instance" "docdb_r5_xlarge" {
  instance_class = "db.r5.xlarge"
}
resource "aws_neptune_cluster_instance" "neptune_r5_large" {
  instance_class = "db.r5.large"
}

# --- Analytics / streaming ---
resource "aws_msk_cluster" "this" {
  number_of_broker_nodes = 1
  broker_node_group_info {
    instance_type = "kafka.m5.large"
  }
}
resource "aws_redshift_cluster" "this" {
  node_type = "ra3.xlplus"
  number_of_nodes = 1
}
resource "aws_opensearch_domain" "this" {
  cluster_config {
    instance_type = "t3.small.search"
    instance_count = 1
  }
}

# --- Misc flat/quantity rates ---
resource "aws_secretsmanager_secret" "this" {}
resource "aws_kms_key" "this" {}
resource "aws_wafv2_web_acl" "this" {
  rule {}
  rule {}
}
resource "aws_eks_cluster" "this" {}
resource "aws_eks_node_group" "this" {
  instance_types = ["t3.medium"]
  disk_size = 20
}
resource "aws_route53_zone" "this" {}
resource "aws_db_proxy" "this" {}
