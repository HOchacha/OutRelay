# modules/artifact-bucket
#
# Per-run S3 bucket holding the cross-compiled binaries and the
# dev-PKI material. EC2 user_data fetches from here using the
# instance profile created in the host modules. force_destroy = true
# is intentional: the trap in run.sh always tries to destroy the
# bucket, and we'd rather drop unfinished uploads than leak the
# bucket forever.

locals {
  # AWS S3 bucket names are globally unique and must be lowercase.
  # account_id keeps two operators in different accounts from
  # colliding on the same RUN_ID.
  bucket_name = "outrelay-smoke-${var.run_id}-${data.aws_caller_identity.current.account_id}"
}

data "aws_caller_identity" "current" {}

resource "aws_s3_bucket" "this" {
  bucket        = local.bucket_name
  force_destroy = true
  tags          = merge(var.common_tags, { Name = local.bucket_name })
}

# A 1-day lifecycle rule is the last-resort cleanup if both the
# trap-driven destroy in run.sh and the cleanup-stale.sh safety net
# fail. S3 itself reaps the contents.
resource "aws_s3_bucket_lifecycle_configuration" "expire" {
  bucket = aws_s3_bucket.this.id

  rule {
    id     = "expire-after-1d"
    status = "Enabled"

    filter {}

    expiration {
      days = 1
    }
  }
}

resource "aws_s3_bucket_public_access_block" "block" {
  bucket                  = aws_s3_bucket.this.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Walk artifacts_dir and upload every file. fileset() is recursive
# when given **/*, and aws_s3_object's content_md5 / etag changes
# trigger a re-upload if a binary changes between runs.
locals {
  artifact_files = fileset(var.artifacts_dir, "**/*")
}

resource "aws_s3_object" "artifact" {
  for_each = local.artifact_files

  bucket = aws_s3_bucket.this.id
  key    = each.value
  source = "${var.artifacts_dir}/${each.value}"
  etag   = filemd5("${var.artifacts_dir}/${each.value}")

  tags = var.common_tags
}

# IAM policy document granting GetObject scoped to this bucket only.
# Host modules attach this to their instance profile so each EC2
# can only pull from its own run's artifacts.
data "aws_iam_policy_document" "fetch" {
  statement {
    actions   = ["s3:GetObject", "s3:ListBucket"]
    resources = [aws_s3_bucket.this.arn, "${aws_s3_bucket.this.arn}/*"]
  }
}

resource "aws_iam_policy" "fetch" {
  name   = "outrelay-smoke-${var.run_id}-fetch"
  policy = data.aws_iam_policy_document.fetch.json
  tags   = var.common_tags
}
