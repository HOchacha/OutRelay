variable "run_id" {
  description = "Per-run identifier; included in the bucket name to keep parallel runs from colliding."
  type        = string
}

variable "artifacts_dir" {
  description = "Local directory containing bin/ and pki/ from build-binaries.sh. Each file is uploaded as an aws_s3_object."
  type        = string
}

variable "common_tags" {
  description = "Tags applied to bucket and uploaded objects. The cleanup-stale safety net relies on owner and expires-at."
  type        = map(string)
}
