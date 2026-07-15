---
title: Anonymous usage statistics
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 4
---

# Anonymous usage statistics

`gcx` can report anonymous, non-sensitive usage statistics about how the CLI itself is used to Grafana Labs. This data helps the team understand which commands and flags are used most, where commands fail, and which commands people try that don't exist, so development effort goes where it matters.

The statistics describe only the *shape* of usage — the command path, the names of flags you set, and a categorized outcome. They never include the *content* of what you did: no argument values, no flag values, no resource names, and nothing that identifies you, your organization, or your data.

{{< admonition type="note" >}}
Usage statistics reporting is currently **disabled by default**. If it becomes enabled by default in a future release, that change will be announced in the release notes, and the opt-out controls described on this page will continue to work.
{{< /admonition >}}

## Anonymity

The reports are anonymous:

- The only identifier is a **device ID**: a randomly generated UUID created on first use and stored at `$XDG_STATE_HOME/gcx/device-id`. It identifies an installation of `gcx`, not a person. It's random, never derived from your hardware or account, and never linked to a Grafana or GitHub identity.
- Deleting the device ID file resets it: the next invocation generates a fresh random ID with no connection to the old one.
- If the state directory isn't writable, `gcx` uses a throwaway ID for that invocation only and marks it as not persisted, so those IDs can be excluded from installation counts.
- The data improves `gcx` only. It's never used for billing, entitlements, or auditing.

## What is collected

Each invocation of `gcx` emits at most one event with the following fields:

| Field | Description | Example |
|-------|-------------|---------|
| `service` | Always `gcx`, identifying the reporting product. | `gcx` |
| `version` | The `gcx` version. | `0.4.1` |
| `os` | Operating system. | `linux`, `darwin`, `windows` |
| `arch` | CPU architecture. | `amd64`, `arm64` |
| `device_id` | The random per-installation ID described in [Anonymity](#anonymity). | UUID |
| `device_id_persisted` | Whether the device ID was read from or saved to disk. `false` means a throwaway ID was used for this invocation. | `true` |
| `command` | The resolved command path only — never its arguments. | `dashboards push` |
| `flags` | The **names** of the flags you set, sorted — never their values. | `dry-run,folder` |
| `provider` | The resource provider the command belongs to. | `dashboards` |
| `outcome` | How the invocation ended: `ok`, `runtime_error`, `parse_error`, or `help`. | `ok` |
| `exit_code` | The process exit code. | `0` |
| `error_kind` | A coarse error category when the command failed, such as `auth` or `validation`. Never an error message. | `auth` |
| `duration_ms` | Total invocation duration in milliseconds. | `1234` |
| `is_tty` | Whether `gcx` ran attached to an interactive terminal. | `false` |
| `is_ci` | Whether a CI environment was detected. | `true` |
| `ci_provider` | Which CI system was detected, from a fixed list of known names. `gcx` reads well-known CI environment variables to detect the provider but never sends their values. | `github_actions` |
| `is_agent` | Whether an AI coding agent drove the invocation. | `true` |
| `agent` | The name of the agent harness, if one was detected. | `claude-code` |
| `target_kind` | Whether the target Grafana is `cloud` or `self-managed`. Deliberately coarse — never the URL, hostname, or stack slug. | `cloud` |
| `output_format` | The output format the command used. | `table`, `json` |

When the invocation fails to parse — an unknown command, unknown flag, or invalid arguments — these additional fields are set. They capture what was *attempted* so the team can find gaps between what people expect and what exists:

| Field | Description | Example |
|-------|-------------|---------|
| `parse_error_kind` | The kind of parse failure: `unknown_command`, `unknown_flag`, or `invalid_args`. | `unknown_command` |
| `parse_error_parent` | The deepest valid command reached before the failure. | `dashboards` |
| `parse_error_token` | The first unknown token — the guess. It's only sent if it looks like a command name (short, lowercase, no digits, not a URL, IP address, or UUID); otherwise it's replaced with `<redacted>`. | `serch` |
| `attempted_command` | The parent command plus the unknown token, truncated at the unknown token so no later arguments are included. | `dashboards serch` |
| `parse_error_flags` | The **names** of unknown flags — never their values. | `verbsoe` |
| `parse_error_nearest` | The nearest real command or flag name, if one is close. | `search` |
| `parse_error_distance` | The edit distance to the nearest real name, or `-1` if there is no near match. | `2` |

## What is never collected

The following are deliberately never collected, and this is enforced by tests:

- Argument values and flag values
- Resource names, UIDs, or query bodies
- Hostnames, server URLs, and Grafana Cloud stack slugs
- Organization or account identifiers
- Tokens, credentials, or file paths
- Error messages and stack traces
- Your IP address or anything derived from it on the client

## Invocations that report nothing

Some invocations never emit an event:

- **Shell completion** — the completion machinery runs on every tab-press and carries no usage signal.
- **`gcx version`**
- **The `gcx telemetry` command itself** — the command that controls reporting doesn't report on itself.
- **Cancelled invocations** — pressing Ctrl-C emits nothing.

## Server-side enrichment

Reports are received by Grafana's usage-stats service, the same service that receives anonymous usage reports from Grafana, Loki, Tempo, and Mimir. On receipt, the service adds two pieces of information derived from the connection:

- A coarse **geographic region** (for example, a country or subdivision), taken from headers added by the CDN edge.
- The **network organization name** from a whois lookup of the connecting IP address. For CLI traffic this typically resolves to your ISP or employer's network.

The IP address itself is not stored — only the derived region and organization name.

## Inspect what would be sent

To see exactly what `gcx` would report for an invocation, set `GCX_TELEMETRY=log`. The event is printed to stderr and nothing is sent:

```shell
GCX_TELEMETRY=log gcx dashboards list
```

## Opt out

You can control usage statistics reporting three ways. Precedence, highest first:

1. **`GCX_TELEMETRY` environment variable** — set to `enabled`, `disabled`, or `log`. Takes precedence over everything else:

   ```shell
   export GCX_TELEMETRY=disabled
   ```

1. **`DO_NOT_TRACK` environment variable** — set to `1` or `true` to disable reporting, following the cross-tool [DO_NOT_TRACK](https://consoledonottrack.com/) convention. Overridden by `GCX_TELEMETRY`.

1. **Configuration file** — add a top-level `diagnostics` block to your `gcx` configuration file, with `telemetry` set to `enabled`, `disabled`, or `log`:

   ```yaml
   diagnostics:
     telemetry: disabled
   ```

Opting out disables reporting entirely — no event is constructed and nothing is sent.
