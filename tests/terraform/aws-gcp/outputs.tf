output "instance_targets" {
  description = "List of <unit-suffix>:<instance-id>. wait-ready.sh polls each via SSM. The GCP VM is omitted — Cloud-side wait happens through the consumer's curl path (assertion 08 retries with budget) since SSM is AWS-only."
  value = [
    "controller:${module.control_host.instance_id}",
    "relay:${module.control_host.instance_id}",
    "provider-nat:${module.agent_provider_nat.instance_id}",
    "consumer:${module.agent_consumer.instance_id}",
  ]
}

output "control_instance_id" {
  value = module.control_host.instance_id
}

output "consumer_instance_id" {
  value = module.agent_consumer.instance_id
}

output "provider_nat_instance_id" {
  value = module.agent_provider_nat.instance_id
}

output "relay_endpoint" {
  value = module.control_host.relay_endpoint
}

output "bucket_name" {
  value = module.artifacts.bucket_name
}

# Empty in aws-only; populated here. Assertion 06 keys off this to
# decide whether to skip (no EIP provider) and assertion 08 keys off
# provider_gcp_public_ip to confirm the GCP provider exists.
output "provider_eip_instance_id" {
  value = ""
}

output "provider_gcp_instance_id" {
  value = google_compute_instance.agent_provider_gcp.instance_id
}

output "provider_gcp_private_ip" {
  description = "Internal-only IP. With B2 Cloud NAT + EIM the provider has no public IP; consumer reaches it via the Cloud NAT external mapping that srflx auto-discovery surfaces."
  value       = google_compute_instance.agent_provider_gcp.network_interface[0].network_ip
}

output "gcs_bucket" {
  value = google_storage_bucket.artifacts.name
}

# Generic curl target — aws-gcp points at svc-nat (30002) since
# there is no EIP provider in this configuration.
output "primary_url" {
  value = "http://127.0.0.1:30002/"
}

# aws-gcp doesn't deploy the TCP-fallback consumer. Empty signals
# assertion 09 to skip.
output "consumer_tcp_instance_id" {
  value = ""
}

output "provider_remote_instance_id" {
  value = ""
}

output "forward_endpoint" {
  value = ""
}
