# Security

## Reporting a vulnerability

Please report security issues privately rather than in a public issue. Use
GitHub's private vulnerability reporting on this repository, or email
`security@devoplus.com.tr`.

Include what you found, how to reproduce it, and what an attacker could do
with it. Please do not include real API keys or tokens.

## What the provider handles

The provider is invoked by GARM once per operation and holds two kinds of
secret while it runs:

- **A Vultr API key**, read from the provider config file or from the
  `VULTR_API_KEY` environment variable. It is sent to the Vultr API as a
  bearer token and is never written to stdout, stderr or any log.
- **The GARM instance token and runner bootstrap**, received on stdin and
  passed to the instance as cloud-init user data.

## Operating it safely

- Keep the provider config file readable only by the user GARM runs as
  (`chmod 0600`). Prefer leaving `api_key` empty and passing the key through
  `VULTR_API_KEY`, which GARM forwards only when the provider section lists it
  in `environment_variables`.
- Give the API key only the permissions it needs to manage instances.
- The provider tags every instance with the GARM controller id and refuses to
  delete, start, stop or report an instance that does not carry it. Several
  GARM installations can share a Vultr account without reaching each other's
  runners. Removing or rewriting those tags by hand breaks that boundary.
- Self-hosted runners execute workflow code. GitHub recommends using them
  with private repositories only. Attaching a Vultr firewall group that
  restricts inbound access, and keeping runners in a VPC, limits what a
  compromised runner can reach.
