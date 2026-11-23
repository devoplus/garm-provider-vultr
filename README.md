# garm-provider-vultr

A [GARM](https://github.com/cloudbase/garm) external provider that runs GitHub
Actions runners on [Vultr](https://www.vultr.com) compute instances.

GARM calls this binary to create, list, inspect, start, stop and delete the
instances that host self-hosted runners. The provider implements the
`v0.1.1` external provider interface, which is a superset of `v0.1.0`, so it
works with either interface version.

GARM 1.x currently invokes the seven lifecycle operations
(`CreateInstance`, `DeleteInstance`, `GetInstance`, `ListInstances`,
`RemoveAllInstances`, `StartInstance`, `StopInstance`). The remaining
`v0.1.1` operations — `GetVersion`, `GetSupportedInterfaceVersions`,
`ValidatePoolInfo`, `GetConfigJSONSchema` and `GetExtraSpecsJSONSchema` — are
implemented and can be called directly, but GARM does not use them yet.

## Status

| | |
| --- | --- |
| Provider interface | `v0.1.0` and `v0.1.1` |
| Runner operating systems | Linux |
| Vultr API | v2 (`https://api.vultr.com`) |
| Live verification | Full lifecycle exercised against the real Vultr API |

The create, get, list, stop, start and delete operations have been run against
a live Vultr account, including the cases the provider contract depends on: a
deleted instance reported as Not Found with exit code 30, a repeated delete as
a no-op, and an instance owned by another controller id staying invisible in a
shared account.

Windows pools are rejected with a clear error. GARM delivers the runner
bootstrap through cloud-init user data; Vultr Linux images run cloud-init, but
whether Vultr Windows images process user data the same way has not been
verified, so a Windows pool fails fast instead of producing an instance that
never registers a runner.

## Building

```bash
go build -o garm-provider-vultr .
```

For a static binary suitable for the GARM container image:

```bash
./scripts/build-static.sh
```

Install the binary where GARM can reach it, for example
`/opt/garm/providers.d/garm-provider-vultr`.

## Configuration

### Provider configuration

Create `/etc/garm/garm-provider-vultr.toml`:

```toml
# A Vultr API key with permission to manage instances. Leave it empty to read
# the key from the VULTR_API_KEY environment variable instead, which keeps it
# out of this file.
api_key = ""

# The Vultr region runners are created in. See GET /v2/regions.
region = "ewr"

# Everything below is optional and can be overridden per pool through
# extra_specs.

# sshkey_ids = ["00000000-0000-0000-0000-000000000000"]
# firewall_group_id = "00000000-0000-0000-0000-000000000000"
# script_id = "00000000-0000-0000-0000-000000000000"
# enable_ipv6 = false
# disable_public_ipv4 = false     # requires enable_ipv6
# enable_vpc = false
# vpc_ids = ["00000000-0000-0000-0000-000000000000"]
# vpc_only = false                # requires vpc_ids with a NAT gateway
# ddos_protection = false
# backups = "disabled"            # "enabled" or "disabled"
# user_scheme = "root"            # "root" or "limited"
# tags = ["team-platform"]
```

Keep the file readable only by the user GARM runs as:

```bash
chmod 0600 /etc/garm/garm-provider-vultr.toml
```

### Passing the API key through the environment

The recommended setup leaves `api_key` empty in the config file and hands the
key to the provider through the environment, so it is not stored next to the
rest of the configuration:

```toml
[[provider]]
  name = "vultr"
  provider_type = "external"
  description = "Vultr"
  [provider.external]
    interface_version = "v0.1.1"
    provider_executable = "/opt/garm/providers.d/garm-provider-vultr"
    config_file = "/etc/garm/garm-provider-vultr.toml"
    environment_variables = ["VULTR_API_KEY"]
```

GARM passes a clean environment to providers, so `VULTR_API_KEY` only reaches
the provider when it is listed in `environment_variables`.

An explicit `api_key` in the config file takes precedence over the
environment variable.

### Pool configuration

| Pool field | Vultr meaning | Example |
| --- | --- | --- |
| `flavor` | Plan id. See `GET /v2/plans`. | `vc2-2c-4gb` |
| `image` | What the instance boots. See below. | `1743` |

The `image` field accepts four forms, because the Vultr API offers four ways
to pick what an instance boots:

| Form | Selects | Example |
| --- | --- | --- |
| `<number>` or `os_id:<number>` | An operating system. `GET /v2/os`. | `2284` |
| `snapshot_id:<uuid>` | A snapshot. `GET /v2/snapshots`. | `snapshot_id:cb676a46-...` |
| `app_id:<number>` | A one-click application. `GET /v2/applications`. | `app_id:1234` |
| `image_id:<slug>` | A marketplace application image. | `image_id:docker` |

At the time of writing, `1743` is Ubuntu 22.04 LTS x64 and `2284` is Ubuntu
24.04 LTS x64, but these ids are not stable.

Vultr documents that os and plan ids change over time, so read the current
values from the API rather than copying them from a guide:

```bash
curl -s "https://api.vultr.com/v2/os?per_page=500" \
  -H "Authorization: Bearer ${VULTR_API_KEY}" | jq '.os[] | select(.family=="ubuntu")'

curl -s "https://api.vultr.com/v2/plans?per_page=500" \
  -H "Authorization: Bearer ${VULTR_API_KEY}" | jq '.plans[] | {id, vcpu_count, ram}'
```

Creating a pool, either from the GARM web UI or from the CLI:

```bash
garm-cli pool create \
  --repo <repo-id> \
  --provider-name vultr \
  --image 2284 \
  --flavor vc2-2c-4gb \
  --os-type linux \
  --os-arch amd64 \
  --tags vultr,ubuntu \
  --min-idle-runners 0 \
  --max-runners 5 \
  --enabled true
```

### Extra specs

Any of the following can be set per pool with `--extra-specs`. Unknown fields
are rejected, so a typo surfaces when the pool is created rather than when the
first runner fails.

```json
{
  "region": "fra",
  "hostname": "runner-01",
  "sshkey_ids": ["00000000-0000-0000-0000-000000000000"],
  "firewall_group_id": "00000000-0000-0000-0000-000000000000",
  "script_id": "00000000-0000-0000-0000-000000000000",
  "reserved_ipv4": "00000000-0000-0000-0000-000000000000",
  "tags": ["pool-tag"],
  "enable_ipv6": true,
  "disable_public_ipv4": false,
  "enable_vpc": false,
  "vpc_ids": ["00000000-0000-0000-0000-000000000000"],
  "vpc_only": false,
  "ddos_protection": false,
  "backups": "disabled",
  "user_scheme": "root",
  "disable_updates": false,
  "enable_boot_debug": false,
  "extra_packages": ["jq"]
}
```

`sshkey_ids`, `vpc_ids` and `extra_packages` replace the values from the
provider configuration; `tags` are added to them.

The provider can also print both schemas itself. GARM does not request them
yet, but the operations are implemented:

```bash
GARM_INTERFACE_VERSION=v0.1.1 \
GARM_COMMAND=GetExtraSpecsJSONSchema \
GARM_CONTROLLER_ID=schema-dump \
GARM_PROVIDER_CONFIG_FILE=/etc/garm/garm-provider-vultr.toml \
  /opt/garm/providers.d/garm-provider-vultr
```

## How the provider identifies its instances

Every instance is created with four Vultr tags:

| Tag | Purpose |
| --- | --- |
| `garm-controller-id-<id>` | The GARM installation that owns the instance. |
| `garm-pool-id-<id>` | The pool the runner belongs to. |
| `garm-os-type-<type>` | Preserves the runner OS type. |
| `garm-os-arch-<arch>` | Preserves the runner architecture. |

The controller tag is a hard boundary: the provider never deletes, starts,
stops or reports an instance that does not carry the controller id it was
invoked with. Several GARM installations can therefore share one Vultr
account without touching each other's runners.

The Vultr list endpoint can only filter on a deprecated singular `tag` query
parameter, so `ListInstances` reads the account listing with cursor
pagination and filters on these tags itself.

## Instance status

GARM accepts only four statuses from a provider. The Vultr states map onto
them as follows:

| Vultr `status` / `power_status` | Reported to GARM |
| --- | --- |
| `pending`, `resizing` | `running` — the instance exists and is on its way |
| `active` / `running` | `running` |
| `active` / `stopped` | `stopped` |
| `suspended` | `error`, with a provider fault explaining it |
| anything else | `unknown` |

## Rate limits and retries

The Vultr API rate limits at 30 requests per second and answers with `429`
and a `Retry-After` header. The provider honours that header, retries rate
limited and server side failures with an exponential backoff, and never
retries a rejected request. A `404` is reported to GARM as a Not Found, which
is what makes `DeleteInstance` a no-op for an instance that is already gone.

## Development

```bash
go test ./...
go vet ./...
```

The HTTP client is covered against an `httptest` server, and the provider
lifecycle against a fake API client, so the suite needs no Vultr credentials
and makes no network calls.

## License

Apache License 2.0. See [LICENSE](LICENSE).
