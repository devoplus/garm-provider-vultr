# Contributing

Thanks for your interest in this provider.

## Reporting problems

Open an issue describing what you expected, what happened, and which GARM and
provider versions you are running. `garm-cli provider show vultr` reports the
provider version.

Never paste a Vultr API key, a GARM instance token or a runner registration
token into an issue. Redact them first.

## Pull requests

- Keep a pull request to one change.
- `make verify` has to pass. It runs `go vet` and the test suite.
- New behaviour comes with a test. The suite runs without Vultr credentials
  and makes no network calls: the HTTP client is tested against an `httptest`
  server, and the provider lifecycle against a fake API client. Please keep it
  that way.
- Anything the provider relies on in the Vultr API should be verifiable from
  the [Vultr API documentation](https://www.vultr.com/api/). When a behaviour
  is not documented, say so in a comment next to the code that depends on it
  rather than leaving it implied.
- Document user visible changes in `README.md`.

## Scope

This provider implements the GARM external provider interface. The interface
itself is defined in
[cloudbase/garm](https://github.com/cloudbase/garm/blob/main/doc/external_provider.md)
and in
[cloudbase/garm-provider-common](https://github.com/cloudbase/garm-provider-common);
changes to the interface belong there, not here.

## License

By contributing you agree that your contribution is licensed under the
Apache License 2.0, as the rest of this repository is.
