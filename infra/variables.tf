variable "alarm_email" {
  description = "Recipient for AWS Budgets alarms. Set in terraform.tfvars (not committed)."
  type        = string
}

variable "monthly_budget_usd" {
  description = "Monthly cost ceiling for the budget alarm, in USD."
  type        = string
  default     = "5.0"
}
