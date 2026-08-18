CATALOG_REGIONS ?= us-east-1 us-east-2 us-west-1 us-west-2 eu-west-1 eu-west-2 eu-central-1 ap-northeast-1 ap-northeast-2 ap-southeast-1
CATALOG_DIR := internal/catalog
CATALOG_BIN := $(shell mktemp -d)

# Regenerate the embedded price catalog. Requires AWS credentials (Price List
# API). Runs the tool itself over testdata/catalog/main.tf once per region; a
# resource added to that file is automatically covered on the next run.
.PHONY: catalog
catalog:
	go build -o $(CATALOG_BIN)/terraform-price ./cmd/terraform-price
	rm -f $(CATALOG_DIR)/prices.json
	for region in $(CATALOG_REGIONS); do \
		echo "==> $$region"; \
		work=$$(mktemp -d); \
		cp testdata/catalog/main.tf $$work/; \
		printf 'aws_region = "%s"\n' $$region > $$work/terraform.tfvars; \
		$(CATALOG_BIN)/terraform-price --price-file $(CURDIR)/$(CATALOG_DIR)/prices.json --format compact $$work > /dev/null || exit 1; \
		rm -rf $$work; \
	done
	date +%F > $(CATALOG_DIR)/date.txt
	@echo "==> $$(python3 -c 'import json;print(len(json.load(open("$(CATALOG_DIR)/prices.json"))))') prices in $(CATALOG_DIR)/prices.json"

.PHONY: test
test:
	go test ./...
