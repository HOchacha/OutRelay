output "bucket_name" {
  value = aws_s3_bucket.this.id
}

output "fetch_policy_arn" {
  description = "Attach this to the EC2 instance profile so user_data can aws s3 cp from the bucket."
  value       = aws_iam_policy.fetch.arn
}
