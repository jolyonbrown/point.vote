output "side_door_url" {
  description = "The RSA-compat URL to hand to clients behind legacy TLS-inspection proxies."
  value       = "https://${aws_cloudfront_distribution.rsa_side_door.domain_name}"
}
