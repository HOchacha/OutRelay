# aws-gcp/
#
# Same AWS half as aws-only, MINUS the EIP-attached provider. The
# GCP-side provider replaces it as a cross-cloud test target.
#
# +-----------------------------+        +------------------------------+
# | AWS VPC control   (public)  |        | GCP VPC provider             |
# |   EC2: controller + relay   |        |   Cloud NAT (EIM=on)         |
# |        EIP, SG: 7443/udp    |        |   GCE: agent (provider role) |
# |                             |        |        no public IP, only    |
# |                             |        |        private + Cloud NAT   |
# +-----------------------------+        +------------------------------+
#
# +-----------------------------+        +------------------------------+
# | AWS VPC consumer            |        | AWS VPC provider-nat         |
# |   private subnet + NAT GW   |        |   private subnet + NAT GW    |
# |   EC2: agent (consumer)     |        |   EC2: agent (provider-nat)  |
# +-----------------------------+        +------------------------------+
#
# B2: the GCP provider sits behind Cloud NAT with endpoint-
# independent mapping (EIM) — no public IP. Combined with the
# agent's shared UDP socket (transport.SharedTransport), this
# exercises the full §3.19 hole-punching path: provider's outbound
# QUIC to the relay determines the NAT mapping for its only socket;
# srflx tells the agent that mapping; consumer dials it; EIM means
# Cloud NAT accepts the inbound regardless of source endpoint and
# routes back to the same socket where quic-go's listener picks it
# up. Symmetric ↔ EIM is the contribution C2 narrative.

resource "time_static" "now" {}

locals {
  azs             = ["${var.aws_region}a", "${var.aws_region}c"]
  expires_at_unix = time_static.now.unix + var.expires_after_hours * 3600

  common_tags = {
    owner        = var.owner_tag
    "run-id"     = var.run_id
    "expires-at" = tostring(local.expires_at_unix)
  }

  # GCP labels: lowercase + alphanumerics + hyphens only. Strip the
  # AWS tag dialect to fit, and drop the colons in expires-at.
  gcp_labels = {
    owner      = var.owner_tag
    run_id     = lower(replace(var.run_id, ".", "-"))
    expires_at = tostring(local.expires_at_unix)
  }
}

data "aws_ssm_parameter" "al2023" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"
}

# ---- AWS-side artifact bucket ------------------------------------------

module "artifacts" {
  source = "../modules/artifact-bucket"

  run_id        = var.run_id
  artifacts_dir = var.artifacts_dir
  common_tags   = local.common_tags
}

# ---- AWS VPCs ----------------------------------------------------------

module "vpc_control" {
  source = "../modules/vpc"

  name        = "control"
  cidr        = "10.10.0.0/24"
  azs         = local.azs
  common_tags = local.common_tags
}

module "vpc_provider_nat" {
  source = "../modules/vpc"

  name             = "provider-nat"
  cidr             = "10.30.0.0/24"
  azs              = local.azs
  with_nat_gateway = true
  common_tags      = local.common_tags
}

module "vpc_consumer" {
  source = "../modules/vpc"

  name             = "consumer"
  cidr             = "10.40.0.0/24"
  azs              = local.azs
  with_nat_gateway = true
  common_tags      = local.common_tags
}

# ---- Control host + AWS-side agents -----------------------------------

module "control_host" {
  source = "../modules/control-host"

  run_id           = var.run_id
  vpc_id           = module.vpc_control.vpc_id
  subnet_id        = module.vpc_control.public_subnet_id
  ami_id           = data.aws_ssm_parameter.al2023.value
  bucket_name      = module.artifacts.bucket_name
  fetch_policy_arn = module.artifacts.fetch_policy_arn
  common_tags      = local.common_tags
}

module "agent_provider_nat" {
  source = "../modules/agent-host"

  run_id             = var.run_id
  role               = "provider-nat"
  vpc_id             = module.vpc_provider_nat.vpc_id
  subnet_id          = module.vpc_provider_nat.private_subnet_id
  ami_id             = data.aws_ssm_parameter.al2023.value
  bucket_name        = module.artifacts.bucket_name
  fetch_policy_arn   = module.artifacts.fetch_policy_arn
  relay_endpoint     = module.control_host.relay_endpoint
  agent_uuid         = "00000000-0000-0000-0000-000000000003"
  cert_prefix        = "agent-provider-nat"
  agent_mode         = "provider"
  service_name       = "svc-nat"
  attach_eip         = false
  allow_inbound_quic = false
  common_tags        = local.common_tags
}

module "agent_consumer" {
  source = "../modules/agent-host"

  run_id             = var.run_id
  role               = "consumer"
  vpc_id             = module.vpc_consumer.vpc_id
  subnet_id          = module.vpc_consumer.private_subnet_id
  ami_id             = data.aws_ssm_parameter.al2023.value
  bucket_name        = module.artifacts.bucket_name
  fetch_policy_arn   = module.artifacts.fetch_policy_arn
  relay_endpoint     = module.control_host.relay_endpoint
  agent_uuid         = "00000000-0000-0000-0000-000000000002"
  cert_prefix        = "agent-consumer"
  agent_mode         = "consumer"
  service_name       = "svc-nat"
  consume_bind_addr  = "127.0.0.1:30002"
  extra_consumes     = ["svc-gcp@127.0.0.1:30003"]
  attach_eip         = false
  allow_inbound_quic = false
  common_tags        = local.common_tags
}

# ---- GCP side ---------------------------------------------------------
#
# Network: a single VPC + subnet in the chosen region. The VM has
# NO public IP; all egress goes through Cloud NAT (configured for
# endpoint-independent mapping below).

resource "google_compute_network" "provider" {
  name                    = "outrelay-${var.run_id}-provider"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "provider" {
  name                     = "outrelay-${var.run_id}-provider"
  network                  = google_compute_network.provider.id
  region                   = var.gcp_region
  ip_cidr_range            = "10.50.0.0/24"
  private_ip_google_access = true
}

# Cloud Router + Cloud NAT with endpoint-independent mapping. EIM
# means the same internal endpoint (ip:port) maps to the same
# external endpoint regardless of destination — packets from any
# source landing on the external endpoint are routed back to the
# internal socket. Without EIM, Cloud NAT defaults to per-
# destination mapping (effectively symmetric NAT) and inbound from
# anywhere-but-the-original-destination is dropped.
resource "google_compute_router" "provider" {
  name    = "outrelay-${var.run_id}-router"
  network = google_compute_network.provider.id
  region  = var.gcp_region
}

resource "google_compute_router_nat" "provider" {
  name                                = "outrelay-${var.run_id}-nat"
  router                              = google_compute_router.provider.name
  region                              = var.gcp_region
  nat_ip_allocate_option              = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat  = "ALL_SUBNETWORKS_ALL_IP_RANGES"
  enable_endpoint_independent_mapping = true
  # EIM is incompatible with dynamic port allocation; the GCP API
  # rejects (true, true). With EIM the agent gets a fixed external
  # port for its socket — exactly the property §3.19 needs.
  enable_dynamic_port_allocation = false
  min_ports_per_vm               = 64
}

# Inbound UDP for the agent's --p2p-listen on 7445. Even though the
# VM has no public IP, Cloud NAT's port mapping forwards to the VM's
# private interface; this firewall rule is what lets those packets
# actually reach the listener once they're on the VM's NIC.
resource "google_compute_firewall" "agent_inbound" {
  name        = "outrelay-${var.run_id}-agent-inbound"
  network     = google_compute_network.provider.name
  description = "outrelay smoke agent: inbound QUIC for §3.19 P2P direct dials"
  direction   = "INGRESS"

  allow {
    protocol = "udp"
    ports    = ["1024-65535"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["outrelay-agent"]
}

# ---- GCS artifact bucket (binaries + PKI) -----------------------------

# Object names from fileset(...) carry slashes; storage object names
# accept that natively.
locals {
  gcs_bucket_name = lower("outrelay-smoke-${var.run_id}-${var.gcp_project}")
  artifact_files  = fileset(var.artifacts_dir, "**/*")
}

resource "google_storage_bucket" "artifacts" {
  name                        = local.gcs_bucket_name
  location                    = var.gcp_region
  force_destroy               = true
  uniform_bucket_level_access = true

  lifecycle_rule {
    condition {
      age = 1
    }
    action {
      type = "Delete"
    }
  }

  labels = local.gcp_labels
}

resource "google_storage_bucket_object" "artifact" {
  for_each = local.artifact_files

  bucket = google_storage_bucket.artifacts.name
  name   = each.value
  source = "${var.artifacts_dir}/${each.value}"
}

# ---- Service account for the GCE provider VM --------------------------

resource "google_service_account" "agent" {
  account_id   = "outrelay-${substr(var.run_id, 0, 22)}"
  display_name = "OutRelay smoke agent ${var.run_id}"
}

resource "google_storage_bucket_iam_member" "agent_read" {
  bucket = google_storage_bucket.artifacts.name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.agent.email}"
}

# ---- GCE provider VM --------------------------------------------------

resource "google_compute_instance" "agent_provider_gcp" {
  name         = "outrelay-${var.run_id}-provider-gcp"
  machine_type = "e2-small"
  zone         = var.gcp_zone

  tags = ["outrelay-agent"]

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-12"
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.provider.id
    # No access_config — the VM has NO public IP. All egress goes
    # through Cloud NAT (which the §3.19 hole-punch story relies
    # on). Inbound from peers also routes via Cloud NAT's EIM
    # mapping. private_ip_google_access on the subnet keeps the
    # GCS download path alive without sending it through Cloud NAT
    # (Google service IPs are reachable directly from private IPs).
  }

  service_account {
    email = google_service_account.agent.email
    # cloud-platform read-only is enough to read GCS objects;
    # tighter than the broad cloud-platform scope.
    scopes = ["https://www.googleapis.com/auth/devstorage.read_only"]
  }

  metadata_startup_script = templatefile("${path.module}/cloud-init-gcp.tftpl", {
    gcs_bucket     = google_storage_bucket.artifacts.name
    relay_endpoint = module.control_host.relay_endpoint
    tenant         = "acme"
    agent_uuid     = "00000000-0000-0000-0000-000000000004"
    cert_prefix    = "agent-provider-gcp"
    role           = "provider-gcp"
    service_name   = "svc-gcp"
  })

  labels = local.gcp_labels

  # Object uploads + Cloud NAT must be in place before the VM
  # boots — otherwise gsutil cp races on the artifacts and the VM
  # has no internet at all.
  depends_on = [
    google_storage_bucket_object.artifact,
    google_compute_router_nat.provider,
  ]
}
