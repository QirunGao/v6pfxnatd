# Engineering Principles

`v6pfxnatd` is an event-driven nftables IPv6 prefix NAT reconciler for dynamic DHCPv6-PD. The reconciler keeps no state across runs, but the rules it manages implement nftables stateful NAT and therefore use conntrack.

It does only six things:

1. Read one TOML config file.
2. Observe relevant IPv6 route changes through rtnetlink.
3. Read current kernel routes and derive deterministic `/64` mappings from one DHCPv6-PD.
4. Compare one complete desired nftables table with the current managed table.
5. Atomically replace that table only when the desired state changed.
6. After a successful replacement, best-effort publish one read-only operational snapshot.

Everything else is out of scope unless it is the smallest direct fix needed to keep those six things true.

## Hard Boundary

The program owns exactly one configured `ip6` nftables table. The kernel route state and that table are the only runtime facts used for reconciliation. The process must not persist or read applied state between reconciliations. `/run/v6pfxnatd/status` is an output-only operational snapshot.

Forbidden directions:

- No general firewall manager.
- No route manager or DHCPv6 client.
- No configuration planner or executor framework.
- No applied-state file, generation file, PID file, or temporary rule file; the atomic operational status snapshot is the only runtime file.
- No external `ip`, `nft`, or `conntrack` commands.
- No conntrack cleanup, rebuild, or migration.
- No RFC 6296 NPTv6 implementation.
- No guarantee that existing conntrack flows immediately use a newly applied prefix.
- No retry queue or rollback framework.
- No plugin system, database, web UI, metrics platform, or distributed coordination.
- No internal packages or generic netlink abstraction solely for future reuse.

## Allowed Concepts

The allowed concepts are:

- `RawConfig`: TOML input.
- `NormalizedConfig`: validated, immutable desired behavior.
- `NetworkSnapshot`: PD candidates after the configured WAN link and default route have been proved.
- `PrefixMapping`: one internal `/64` and derived external `/64` pair; translation replaces the high 64 bits and preserves the low 64 bits.
- `DesiredSpec`: deterministic table, chain, map, rule, mapping, and operational-address structure.
- `DesiredArtifact`: a desired spec and its fingerprint.
- `CurrentTable`: the current managed nftables objects needed for comparison.

Do not add another layer unless the current pipeline cannot express one of the six required actions without it.

## Pipeline Boundary

The only allowed order is:

```text
load config
  -> subscribe and trigger
  -> read network snapshot
  -> select one PD
  -> derive mappings
  -> build desired spec
  -> fingerprint desired spec
  -> read current table
  -> compare
  -> build replacement
  -> flush once
  -> best-effort atomically publish operational status
```

Every stage has an explicit read boundary and returns a complete value to the next stage. A later stage must not quietly re-read or repair an earlier stage's input.

No nftables mutation may be queued before all route reads, calculations, validation, fingerprinting, current-table reads, and comparison have succeeded.

## Configuration Boundary

The program reads one TOML path at startup and passes it directly to the TOML reader. Missing files, unreadable files, directories, and decode failures fail naturally through the underlying file or decoder error. Configuration is immutable for the process lifetime.

Validation rejects common operator mistakes and values that make deterministic derivation impossible:

- Empty interface or table names.
- Invalid route table, protocol, or type ranges.
- A delegated prefix length outside `0..64`.
- Missing mappings.
- Non-IPv6, non-`/64`, non-masked, or duplicate inside prefixes.
- Duplicate mapping names or subnet IDs.
- A subnet ID that does not fit in `64 - delegated prefix length` bits.
- Invalid or duplicate operational address names, unknown mapping references, or suffixes that contain high 64 bits.
- Unknown log levels or formats.

Validation does not preflight interface existence, route presence, PD uniqueness, nftables availability, or kernel support. Those belong to their runtime read stages and fail naturally there.

## Stateless Reconcile

Route events are trigger signals only. Event payloads are never treated as the selected PD. Every reconciliation reads a fresh, complete kernel snapshot.

The route watcher is long-running. It signals readiness exactly once after the netlink socket and all required subscriptions are active, then continues consuming updates until the context is canceled or the subscription fails. `Run` waits for readiness before enqueuing the initial reconciliation. Runtime watcher failures must reach `Run`; context cancellation must allow a clean exit without being reported as watcher failure.

The process may hold intermediate values during one reconciliation, but it must not keep a last PD, last fingerprint, applied state, generation, or table handle across reconciliations. The operational status file is never read and cannot affect reconciliation.

Temporary route incompleteness is not repaired. A missing default route, missing PD, or multiple valid PD candidates makes the current reconciliation fail without changing nftables. A later route event triggers a fresh attempt.

A failed reconciliation is not retried on a timer and does not guarantee automatic convergence. Without another relevant route event or an administrator restart, no later reconciliation is promised.

## Managed Table

The configured `ip6` table is the only table this program may inspect or replace. A query must explicitly select `nftables.TableFamilyIPv6`. The current-state read only needs the configured table identity and the comments on the managed `dnat_prefixes` and `snat_prefixes` maps.

Those comments carry the desired fingerprint. If both managed map comments match the current desired fingerprint, the table is considered current. Foreign objects or external modifications inside the table are outside the application's responsibility and are not actively detected.

Operational address configuration participates in the desired fingerprint. Changing only `[[addresses]]` therefore performs a real managed-table metadata replacement before publishing the new snapshot; otherwise the unchanged path would correctly refuse to refresh it.

When current and desired states are equal, return success without creating a modifying connection and without calling `Flush()`.

When they differ, queue deletion of the old table, creation of the complete new table, maps, elements, and rules on a fresh connection. Call `Flush()` exactly once.

Only after that `Flush()` succeeds, best-effort atomically replace `/run/v6pfxnatd/status`. A status write failure is logged and does not change reconcile success. The unchanged path and every failed reconcile path leave the existing snapshot untouched.

Each managed map key is an IPv6 `/64` interval. Each map value is the target `/64` encoded as its inclusive minimum and maximum IPv6 addresses. The lookup writes those two 128-bit addresses to registers 1 and 2, and the prefix NAT expression uses them as `RegAddrMin` and `RegAddrMax`. A plain 16-byte `ipv6_addr` value is invalid because it loses the target prefix range and clears the packet's low 64 bits.

The caller owns that fresh modifying connection. `BuildReplacement` receives it as an argument and only queues the complete replacement. It does not create, close, or flush the connection. If construction fails, the caller must not call `Flush()`.

Do not attempt partial repair. Do not inspect unrelated nftables tables. Do not add object-inventory or expression-decompiler logic whose only purpose is detecting external pollution inside the configured table.

## Failure Semantics

Before `Flush()`, every error returns directly and leaves the current nftables table untouched.

If `Flush()` fails, return that error. Do not retry, issue a second deletion, downgrade to per-object writes, or perform manual rollback. nftables transaction semantics prevent exposure of a partially constructed table, but a transport or acknowledgement error does not always prove whether the kernel committed the transaction. A later reconciliation reads kernel state again if another event or administrator restart triggers it.

A successful `Flush()` means the nftables ruleset was replaced. Existing conntrack flows and NAT bindings remain governed by the kernel conntrack lifecycle and are not part of the success condition. Publishing the operational snapshot is a best-effort side effect after that success boundary.

The route watcher failing is process-fatal so systemd can restart the service. A single reconcile failure is logged and the watcher continues.

## Review Rule

A review finding is valid only if it proves one of these direct risks under ordinary operation:

1. The program can submit an nftables change before all prerequisite stages succeed.
2. The unchanged path can call `Flush()` or otherwise mutate nftables incorrectly.
3. A failed pre-commit stage can alter the managed table.
4. The replacement is split across multiple commits or can expose a partial table.
5. Prefix derivation, normalization, ordering, or fingerprinting is nondeterministic.
6. A defaulting family lookup or config value can select or mutate an nftables table other than the configured managed IPv6 table.
7. A normal, plausible configuration cannot complete the documented pipeline because of a program bug.
8. The initial reconciliation can run before route subscription readiness, or the watcher can return immediately after successful subscription, misreport cancellation, or fail without notifying `Run`.
9. `BuildReplacement` can create, submit, or retain ownership of its own nftables connection.
10. Documentation or code treats every `Flush()` error as proof that kernel state is unchanged.

Every accepted finding must name the violated invariant and propose the smallest direct fix. Do not accept architecture whose main purpose is future extensibility.
