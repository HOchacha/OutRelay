# tests/terraform/Makefile
#
# Cloud smoke tests for OutRelay. See README.md for the design notes.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

REPO_ROOT := $(abspath ../..)
ROOT      := $(abspath .)

# Per-run identifier — propagated as the `run_id` Terraform variable
# and as a tag on every cloud resource. Defaults to a date+git-sha
# suffix so multiple runs on the same day don't collide.
RUN_ID ?= smoke-$(shell date -u +%Y%m%d-%H%M%S)

# Resource expiry budget passed to Terraform as `expires_after_hours`.
# The cleanup-stale safety net reaps anything past this budget.
EXPIRES_AFTER_HOURS ?= 2

# AWS region. ap-northeast-2 (Seoul) keeps RTT to a Korean test
# operator low; override for other regions.
AWS_REGION ?= ap-northeast-2

# Owner tag — every resource carries this. The pre-flight check in
# run.sh refuses to apply if it finds prior resources with this tag.
OWNER_TAG ?= outrelay-smoke

export RUN_ID EXPIRES_AFTER_HOURS AWS_REGION OWNER_TAG REPO_ROOT

.PHONY: help smoke-aws smoke-aws-gcp destroy-aws destroy-aws-gcp \
        validate fmt cleanup-stale build-binaries

.DEFAULT_GOAL := help

help: ## show this help
	@awk 'BEGIN{FS=":.*##"; printf "Targets:\n"} \
	     /^[a-zA-Z][a-zA-Z0-9_-]*:.*##/ { \
	       printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 \
	     }' $(MAKEFILE_LIST)

# === Smoke runs =======================================================

smoke-aws: ## build → apply → assert → destroy (AWS-only topology)
	@CONFIG=aws-only $(ROOT)/scripts/run.sh

smoke-aws-gcp: ## same as smoke-aws but adds a GCP Cloud NAT (EIM) provider
	@CONFIG=aws-gcp $(ROOT)/scripts/run.sh

smoke-aws-multi-region: ## two AWS regions: controller+r1+consumer in primary, r2+provider in secondary, validates inter-relay forwarding
	@CONFIG=aws-multi-region $(ROOT)/scripts/run.sh

# === Manual escape hatches ============================================

destroy-aws: ## terraform destroy -auto-approve in aws-only/
	@cd aws-only && terraform destroy -auto-approve \
	  -var "run_id=$(RUN_ID)" \
	  -var "owner_tag=$(OWNER_TAG)" \
	  -var "aws_region=$(AWS_REGION)" \
	  -var "expires_after_hours=$(EXPIRES_AFTER_HOURS)" \
	  -var "artifacts_dir=$(ROOT)/.artifacts"

destroy-aws-gcp: ## terraform destroy -auto-approve in aws-gcp/
	@cd aws-gcp && terraform destroy -auto-approve \
	  -var "run_id=$(RUN_ID)" \
	  -var "owner_tag=$(OWNER_TAG)" \
	  -var "aws_region=$(AWS_REGION)" \
	  -var "expires_after_hours=$(EXPIRES_AFTER_HOURS)" \
	  -var "artifacts_dir=$(ROOT)/.artifacts"

destroy-aws-multi-region: ## terraform destroy -auto-approve in aws-multi-region/
	@cd aws-multi-region && terraform destroy -auto-approve \
	  -var "run_id=$(RUN_ID)" \
	  -var "owner_tag=$(OWNER_TAG)" \
	  -var "aws_region=$(AWS_REGION)" \
	  -var "expires_after_hours=$(EXPIRES_AFTER_HOURS)" \
	  -var "artifacts_dir=$(ROOT)/.artifacts"

cleanup-stale: ## reap any AWS resources tagged owner=$(OWNER_TAG) past their expires-at
	@$(ROOT)/scripts/cleanup-stale.sh

# === Static checks (no cloud API calls) ===============================

validate: ## terraform init + validate in every root module
	@for d in aws-only aws-gcp aws-multi-region; do \
	  echo "=> $$d"; \
	  (cd $$d && terraform init -backend=false -input=false >/dev/null && terraform validate); \
	done

fmt: ## terraform fmt -recursive
	@terraform fmt -recursive

# === Local pre-step ===================================================

build-binaries: ## cross-compile controller/relay/agent/cli for linux/amd64
	@$(ROOT)/scripts/build-binaries.sh
