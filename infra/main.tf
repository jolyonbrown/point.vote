terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

# Credentials come from the environment (AWS_PROFILE); nothing
# account-specific lives in this file.
provider "aws" {
  region = "us-east-1"
}

# The RSA side-door. Some enterprise TLS-inspection stacks (observed:
# iboss) cannot negotiate against Cloudflare's ECDSA-only Universal SSL
# certificate and fail the handshake outright. CloudFront's default
# *.cloudfront.net certificate is RSA, so fronting point.vote with a
# distribution gives those stacks a handshake they can complete, while
# CloudFront itself speaks ECDSA to the Cloudflare edge without
# complaint. Origin Host stays point.vote (the managed
# AllViewerExceptHostHeader policy), which is what routes the request
# at Cloudflare.
resource "aws_cloudfront_distribution" "rsa_side_door" {
  enabled         = true
  is_ipv6_enabled = true
  comment         = "RSA-compat side door for point.vote (legacy TLS-inspection proxies)"
  price_class     = "PriceClass_100"
  # Don't hold the apply open for the ~10-minute global rollout; poll
  # the distribution status instead.
  wait_for_deployment = false

  origin {
    domain_name = "point.vote"
    origin_id   = "pointvote-cf-edge"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    target_origin_id       = "pointvote-cf-edge"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods         = ["GET", "HEAD"]

    # Managed policies: CachingDisabled (rooms are live state; nothing
    # here is safely cacheable) + AllViewerExceptHostHeader (pass
    # everything through, keep Host = origin domain).
    cache_policy_id          = "4135ea2d-6df8-44a3-9df3-4b5a84be39ad"
    origin_request_policy_id = "b689b0a8-53d0-40ab-baf2-68738e2966ac"
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}

# Belt and braces: unlike Cloudflare's free tier, AWS has no spending
# ceiling — a traffic spike bills the card. Page the owner well before
# that becomes interesting.
resource "aws_budgets_budget" "side_door_guard" {
  name         = "pointvote-side-door-guard"
  budget_type  = "COST"
  limit_amount = var.monthly_budget_usd
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 50
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.alarm_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.alarm_email]
  }
}
