# infra — the RSA side-door

> **Status: torn down (2026-07-28), kept as a recipe.** The side-door
> proved the diagnosis in the field — an iboss-managed laptop that
> blocked point.vote loaded the CloudFront URL without complaint — and
> the permanent fix chosen was Cloudflare Advanced Certificate Manager
> (an RSA certificate on point.vote itself), which needs none of this.
> `terraform apply` resurrects the side-door in ~10 minutes if a future
> proxy problem wants the same diagnostic.

Some enterprise TLS-inspection stacks (observed in the wild: iboss)
cannot complete a handshake against Cloudflare's ECDSA-only Universal
SSL certificate and block point.vote as "insecure certificates". This
Terraform stands up a CloudFront distribution whose default
`*.cloudfront.net` certificate is RSA: those proxies get a handshake
they can finish, CloudFront speaks ECDSA onward to the Cloudflare edge,
and the app is none the wiser. A Budgets alarm pages the owner because
AWS, unlike Cloudflare's free tier, has no spending ceiling.

Caveat: SSE streams through CloudFront are cut at its idle timeout and
fall back to the app's reconnect/long-poll path — the side door is for
getting through the wall, not for daily use.

## Usage

```sh
cd infra
cp terraform.tfvars.example terraform.tfvars   # fill in alarm_email
AWS_PROFILE=<profile> terraform init
AWS_PROFILE=<profile> terraform apply
```

`terraform output side_door_url` prints the URL. `terraform destroy`
removes everything.

State (`*.tfstate`) and `terraform.tfvars` are gitignored: state embeds
the AWS account id and distribution internals. Nothing in the committed
files identifies the account.
