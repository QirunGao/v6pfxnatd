# v6pfxnatd

Event-driven nftables IPv6 prefix NAT reconciler for dynamic DHCPv6-PD.

`v6pfxnatd` is a small daemon for Debian/Linux gateways. It observes IPv6 route changes through Linux rtnetlink, reads the current DHCPv6 delegated prefix from kernel routes, derives one public `/64` for each configured internal network, and reconciles a dedicated nftables table through one atomic transaction.

The repository contains the complete reconciler, behavior-focused tests, an example TOML configuration, a systemd unit, packaging metadata, and release workflows. Linux-specific rtnetlink and nftables adapters are isolated with build constraints so the command and its pure pipeline remain buildable on development hosts while the daemon itself reports a clear unsupported-platform error outside Linux.

The configuration format is TOML. The default path is `/etc/v6pfxnatd/config.toml`. Configuration is read once at startup and remains immutable for the process lifetime.

## 1. Goals

Build a long-running, process-stateless, pipeline-oriented Go program that:

1. Observes relevant IPv6 route changes through Linux rtnetlink.
2. Reads the current DHCPv6-PD prefix from kernel routes.
3. Derives a public `/64` for each configured internal network from a fixed subnet ID.
4. Builds the complete desired nftables IPv6 prefix NAT ruleset.
5. Determines whether the current managed table carries the desired fingerprint.
6. Returns successfully without creating a modifying transaction when nothing changed.
7. Atomically replaces the complete dedicated table with one nftables transaction when a change is required.
8. Returns natural errors from every prerequisite stage without modifying the current table.
9. Best-effort publishes a read-only operational snapshot after a successful replacement.

The program does not save the previous prefix, persist applied state, create temporary rule files, or invoke external commands. `/run/v6pfxnatd/status` is output only and is never used as a source of truth.

This program implements IPv6 prefix NAT using nftables stateful NAT. It does not target RFC 6296 NPTv6 and does not require translation to operate independently of conntrack.

Replacing the nftables ruleset determines the rules used by later packets and newly created flows. Existing conntrack flows and their NAT bindings are not managed by this program.

This document defines architecture, stage boundaries, sources of truth, success paths, natural failure semantics, and functional ownership. Exact dependency versions, netlink expression encoding, final log formatting, test implementation, build details, and deployment defaults may be refined during implementation without weakening these boundaries.

## 2. Non-goals and failure semantics

The first version does not:

- Invoke `ip`, `nft`, `conntrack`, or other external commands.
- Save prefix history.
- Save applied state.
- Write or read applied state under `/run`, `/tmp`, or another directory.
- Clean, rebuild, or migrate conntrack state.
- Guarantee that existing conntrack flows immediately use a newly applied prefix.
- Implement RFC 6296 NPTv6.
- Implement a retry queue, timer-based retry, or exponential backoff.
- Implement complex rollback or compensation logic.
- Subscribe to nftables modification events.
- Continuously repair changes made by another nftables manager.
- Rank multiple equivalent DHCPv6-PD candidates.
- Delete the old table when the PD temporarily disappears.
- Design a recovery path for every abnormal network state.
- Guarantee automatic convergence after a failed `Reconcile`.

Prerequisite failures have one meaning:

```text
prerequisite stage fails
    -> return an error
    -> do not create an nftables modification transaction
    -> do not call Flush()
    -> leave the current nftables table unchanged
```

After a failed reconciliation, the daemon does not schedule a retry or run a compensating action. If no later relevant route event arrives and the administrator does not restart the service, another `Reconcile` is not guaranteed.

The administrator is expected to inspect the log, correct external state when necessary, and restart the service if no new route event will retrigger reconciliation. This is an explicit runtime boundary, not a missing recovery feature.

## 3. Core principles

### 3.1 The reconciler keeps no runtime state

Each relevant route event causes the daemon to read again:

- The current WAN interface.
- The current IPv6 default route.
- The current DHCPv6-PD route.
- The current managed nftables table.

Kernel route state and the managed nftables table are the only runtime sources of truth. The operational status snapshot is never read by the daemon.

### 3.2 Events are triggers, not state

An rtnetlink event means only:

```text
IPv6 route state may have changed
```

The event payload is not accepted as the selected delegated prefix. After receiving a relevant event, the daemon queries a fresh and complete route snapshot.

### 3.3 All validation precedes mutation

Before creating a modifying nftables connection, the daemon completes:

- Configuration parsing and validation.
- WAN link and default-route validation.
- Delegated-prefix selection.
- Public-prefix derivation.
- Desired ruleset construction.
- Desired fingerprint calculation.
- Current-table query.
- Current-versus-desired comparison.

Only a valid and demonstrably changed desired state may proceed to transaction construction.

### 3.4 Replacement is the final stage

The only allowed mutation order is:

```text
read
    -> calculate
    -> validate
    -> compare
    -> build one replacement transaction
    -> commit once
```

The current table must not be deleted during a read, calculation, validation, or comparison stage.

### 3.5 Unchanged state performs no commit

The daemon returns success without modification when the configured table exists and both managed map comments carry the current desired fingerprint:

- The selected DHCPv6-PD.
- Every derived public `/64`.
- Every inside IPv6 `/64`.
- The WAN interface name.
- The desired fingerprint stored in `dnat_prefixes` and `snat_prefixes`.

The unchanged path must not delete or create a table, modify a map, replace a rule, create a modifying connection, or call `Flush()`. Operational address configuration is included in the desired fingerprint, so changing only `[[addresses]]` makes the managed metadata changed and permits a post-commit snapshot update.

## 4. Intended Go dependencies

The implementation is expected to use:

```go
github.com/vishvananda/netlink
github.com/google/nftables
github.com/google/nftables/expr
```

`vishvananda/netlink` provides route subscription and route query APIs. Event filtering still belongs to `v6pfxnatd`; route family, table, protocol, type, and prefix-length checks must be explicit.

`google/nftables` talks directly to the nftables netlink API without invoking `nft` or wrapping `libnftnl`. Its connection buffers modification commands, and `Flush()` sends the buffered batch.

nftables batch semantics are the atomic commit boundary. The project must not fall back to sequential per-object commits.

## 5. TOML configuration

The daemon accepts exactly one static TOML configuration file:

```text
/etc/v6pfxnatd/config.toml
```

Another path may be selected with `-c /path/config.toml`. The first version does not watch the file or reload it in place. Configuration changes take effect after a process restart and the startup reconciliation.

The path is passed directly to the TOML reader. Missing files, unreadable files, directories, or other open/decode failures fail naturally through the underlying file and decoder errors. The program validates configuration values after decoding, but it does not enforce absolute paths, symlink policy, or file-mode policy.

Recommended configuration:

```toml
[network]
wan_interface = "ppp-wan1"
policy_route_table = 1001
pd_route_table = 254

[delegated_prefix]
prefix_length = 56
route_protocol = 16
route_type = 7

[nftables]
table_name = "v6pfxnat_wan1"

[[mappings]]
name = "vlan10"
inside_prefix = "fdff:a887:86e4:10::/64"
subnet_id = 0x10

[[mappings]]
name = "vlan20"
inside_prefix = "fdff:a887:86e4:20::/64"
subnet_id = 0x20

[[addresses]]
name = "gateway-c"
mapping = "vlan20"
suffix = "::c"

[logging]
level = "info"
format = "text"
```

`route_protocol` and `route_type` use Linux rtnetlink numeric values. In the example, `16` is `RTPROT_DHCP` and `7` is `RTN_UNREACHABLE`. Numeric values allow direct comparison with kernel fields without maintaining a partial alias parser.

TOML hexadecimal integers are supported. `subnet_id = 0x10` and `subnet_id = 16` are equivalent. Hexadecimal notation is recommended because it makes the bits inserted between a `/56` and `/64` directly visible.

| Field | Meaning | Validation boundary |
| --- | --- | --- |
| `network.wan_interface` | WAN interface used by the default route and prefix NAT rules | Non-empty; runtime existence is checked while reading the network snapshot |
| `network.policy_route_table` | Numeric route table containing the WAN IPv6 default route | Positive valid route-table ID |
| `network.pd_route_table` | Numeric route table containing DHCPv6-PD candidates | Positive valid route-table ID |
| `delegated_prefix.prefix_length` | Length of the delegated prefix | `0..64`; `/56` is the recommended example |
| `delegated_prefix.route_protocol` | rtnetlink protocol field for PD routes | `0..255`; example is `RTPROT_DHCP` |
| `delegated_prefix.route_type` | rtnetlink type field for PD routes | `0..255`; example is `RTN_UNREACHABLE` |
| `nftables.table_name` | Exclusively managed `ip6` table | Non-empty single nftables identifier |
| `mappings[].name` | Stable mapping name | Non-empty and unique |
| `mappings[].inside_prefix` | Internal IPv6 `/64` | IPv6, masked, exactly `/64`, and unique |
| `mappings[].subnet_id` | Value inserted between the PD length and bit 64 | Unique and representable in `64 - prefix_length` bits |
| `addresses[].name` | Shell-safe operational address name | Unique and matches `[A-Za-z0-9._-]+` |
| `addresses[].mapping` | Mapping that supplies the public `/64` | Names an existing mapping |
| `addresses[].suffix` | Interface identifier inserted into the selected public prefix | Unzoned IPv6 address whose high 64 bits are zero |
| `logging.level` | Minimum log level | `debug`, `info`, `warn`, or `error` |
| `logging.format` | Log encoding | `text` or `json` |

At least one `[[mappings]]` entry is required. `[[addresses]]` is optional. After loading, mappings are normalized into deterministic inside-prefix order and addresses into name order. Later stages receive immutable normalized configuration.

The conceptual configuration types are:

```go
type Config struct {
    WANInterface string

    PolicyRouteTable int
    PDRouteTable     int

    PDPrefixLength int
    PDRouteProtocol int
    PDRouteType     int

    NFTTableName string
    Mappings     []MappingConfig
    Addresses    []AddressConfig
    Logging      LoggingConfig
}

type MappingConfig struct {
    Name         string
    InsidePrefix netip.Prefix
    SubnetID     uint64
}

type AddressConfig struct {
    Name    string
    Mapping string
    Suffix  netip.Addr
}

type LoggingConfig struct {
    Level  string
    Format string
}
```

## 6. Reconciliation pipeline

```text
process startup
    |
    +-- load and validate TOML configuration
    |
    +-- establish the IPv6 route subscription
    |
    +-- enqueue one initial reconciliation trigger
            |
            v
       read network snapshot
            |
            v
       select one DHCPv6-PD
            |
            v
       derive public /64 mappings
            |
            v
       build desired ruleset model
            |
            v
       fingerprint desired model
            |
            v
       read current managed table
            |
            v
       compare current and desired
            |
       +----+----+
       |         |
     current   changed
       |         |
       v         v
     return    build replacement
                 |
                 v
              Flush() once
                 |
                 v
              publish status
```

Each stage has a defined input, output, and mutation boundary. No stage may quietly repair an earlier stage's value or reach ahead into a later stage's responsibility.

## 7. Stage 0: load and normalize configuration

### Input

Read the TOML fields described above. This stage performs no kernel or nftables access.

### Validation

Validate that:

- Interface and table names are non-empty.
- Route table IDs are valid.
- The PD prefix length is in `0..64`.
- At least one mapping exists.
- Every inside prefix is masked IPv6 `/64`.
- Inside prefixes, mapping names, and subnet IDs are unique.
- Every subnet ID fits into `64 - PDPrefixLength` bits.
- Operational address names are shell-safe and unique.
- Every operational address names a configured mapping and contains only a low-64-bit IPv6 suffix.
- Logging values are known.

For a `/56`, eight bits are available:

```text
available subnet bits = 64 - 56 = 8
valid SubnetID range  = 0x00..0xff
```

### Output

```go
type NormalizedConfig struct {
    WANInterface string

    PolicyRouteTable int
    PDRouteTable     int

    PDPrefixLength int
    PDRouteProtocol int
    PDRouteType     int

    NFTTableName string
    Mappings     []NormalizedMappingConfig
    Addresses    []NormalizedAddressConfig
    Logging      LoggingConfig
}
```

Mappings and operational addresses are sorted deterministically. No later stage may mutate this value.

### Natural failures

TOML parse errors, invalid IPv6 prefixes, non-`/64` inside prefixes, duplicate mappings, out-of-range subnet IDs, invalid operational addresses, or an invalid managed table name terminate startup. No nftables connection has been created, so the ruleset cannot have changed.

## 8. Stage 1: watch IPv6 route changes

### Subscription

The daemon subscribes to:

```text
RTM_NEWROUTE
RTM_DELROUTE
Family = AF_INET6
```

The implementation may use:

```go
netlink.RouteSubscribeWithOptions(...)
```

`WatchIPv6Routes` is a long-running function. After the netlink socket exists and all required route groups have been subscribed successfully, it closes a one-shot `ready` channel and continues consuming route updates. Readiness is not a return condition.

`Run` must wait for watcher readiness, watcher failure, or context cancellation before it enqueues the initial reconciliation. This closes the startup window in which a route could change after the initial snapshot but before the subscription became active.

The watcher returns only when:

```text
caller cancels ctx
    -> stop normally

route update channel closes
    -> return an error

netlink socket or subscription fails permanently
    -> return an error
```

The implementation may choose its goroutine, channel, and socket layout, but it must satisfy this contract:

```text
continue running after successful subscription
signal readiness exactly once after the subscription is fully active
propagate runtime watcher errors to Run
exit after ctx cancellation
do not stop merely because the subscription initializer returned nil
```

### Event filtering

A default-route event is relevant when:

```text
Family = AF_INET6
Table  = PolicyRouteTable
Dst    = explicit ::/0, or Route.Dst = nil
```

A delegated-prefix event is relevant when:

```text
Family   = AF_INET6
Table    = PDRouteTable
Protocol = PDRouteProtocol
Type     = PDRouteType
DstBits  = PDPrefixLength
```

Unrelated route events are ignored.

### Trigger coalescing

The watcher emits an empty signal through a channel with capacity one:

```go
func Trigger(ch chan<- struct{}) {
    select {
    case ch <- struct{}{}:
    default:
    }
}
```

Bursts are coalesced. If a reconciliation is already running, one pending trigger may remain queued and causes another complete reconciliation afterward.

The watcher does not select a PD, derive mappings, retain event payloads, or modify nftables.

### Natural failures

Subscription setup failure, unexpected route channel closure, or an unrecoverable socket error terminates the watcher and is process-fatal. systemd may then restart the service. An unrelated event is not an error.

## 9. Stage 2: read the network snapshot

```go
func ReadNetworkSnapshot(
    cfg NormalizedConfig,
) (NetworkSnapshot, error)
```

### WAN link

Resolve the configured interface name and use its kernel ifindex to validate the default route. The ifindex is local to this read stage and is not carried into the desired model.

### IPv6 default route

Query `PolicyRouteTable` and prove that at least one IPv6 default route is attached to the WAN ifindex:

```text
Family    = AF_INET6
Table     = PolicyRouteTable
Dst       = explicit ::/0, or Route.Dst = nil
LinkIndex = resolved WAN ifindex
```

An explicit `::/0` and a nil netlink destination are two representations of the same default route and must be treated identically.

One or more matching default routes means success. This stage does not select, rank, or fingerprint a particular default route.

### DHCPv6-PD candidates

Query `PDRouteTable` and filter candidates by:

```text
Family        = AF_INET6
Table         = PDRouteTable
Protocol      = PDRouteProtocol
Type          = PDRouteType
Prefix length = PDPrefixLength
```

Example kernel route:

```text
unreachable 2001:db8:1234:5600::/56 proto dhcp
```

### Output

```go
type NetworkSnapshot struct {
    PDCandidates []netip.Prefix
}
```

Candidate prefixes are converted to masked `netip.Prefix` values. They are not ranked or sorted because any count other than one fails at the next stage.

This stage only describes current kernel facts. It does not choose among multiple candidates, derive public prefixes, query nftables, or modify nftables.

### Natural failures

Return an error when the configured WAN link cannot be read, no matching default route exists, or a route query fails. An empty filtered candidate set is a valid snapshot result and is rejected by the following selection stage. Every failure leaves the managed table unchanged.

## 10. Stage 3: select one delegated prefix

```go
func SelectDelegatedPrefix(
    snapshot NetworkSnapshot,
) (netip.Prefix, error)
```

The first version uses one intentionally strict rule:

```text
exactly one valid PD candidate must exist
```

The network-snapshot stage has already filtered candidates to masked IPv6 prefixes of the configured length. This stage only requires exactly one result; it does not revalidate the internally produced candidate, query the kernel again, rank metrics, guess based on interface state, or remember a previous PD.

No candidates or multiple candidates returns an error without modifying nftables.

## 11. Stage 4: derive public `/64` mappings

```go
func DeriveMappings(
    cfg NormalizedConfig,
    delegatedPrefix netip.Prefix,
) []PrefixMapping
```

For a `/56` delegated prefix, write the eight-bit subnet ID into address bits 56 through 63:

```text
PD:              2001:db8:1234:5600::/56
SubnetID 0x10:   2001:db8:1234:5610::/64
SubnetID 0x20:   2001:db8:1234:5620::/64
```

The result model is:

```go
type PrefixMapping struct {
    Name     string
    Inside   netip.Prefix
    Outside  netip.Prefix
    SubnetID uint64
}
```

Example pairs:

```text
fdff:a887:86e4:10::/64 <-> 2001:db8:1234:5610::/64
fdff:a887:86e4:20::/64 <-> 2001:db8:1234:5620::/64
```

Configuration validation and the route snapshot have already established that the PD and subnet IDs are valid. This pure stage only performs the deterministic bit insertion and does not revalidate those internally produced inputs. It does not access rtnetlink, nftables, or the filesystem.

## 12. Stage 5: build the desired ruleset model

```go
func BuildDesiredSpec(
    cfg NormalizedConfig,
    delegatedPrefix netip.Prefix,
    mappings []PrefixMapping,
) DesiredSpec
```

Build a library-independent model before creating any `google/nftables` object:

```go
type DesiredSpec struct {
    Family    string
    TableName string

    WANInterface   string
    DelegatedPrefix netip.Prefix

    Chains   []ChainSpec
    Maps     []MapSpec
    Rules    []RuleSpec
    Mappings []PrefixMapping
    Addresses []NormalizedAddressConfig
}
```

The example managed table is:

```text
table ip6 v6pfxnat_wan1
```

The fixed chains are:

```text
prerouting:
    type nat
    hook prerouting
    priority dstnat
    policy accept

postrouting:
    type nat
    hook postrouting
    priority srcnat
    policy accept
```

The fixed maps are:

```text
dnat_prefixes: public /64 interval -> inside IPv6 /64 interval
snat_prefixes: inside IPv6 /64 interval -> public /64 interval
```

The key side uses nftables interval-set boundaries so every address in the
source `/64` selects the same element. The value side stores the inclusive
minimum and maximum IPv6 addresses of the target `/64`:

```text
fdff:a887:86e4:10::/64
    -> fdff:a887:86e4:10::
       fdff:a887:86e4:10:ffff:ffff:ffff:ffff
```

The map therefore has the nftables IPv6-address data type with a 32-byte data
length. A plain 16-byte `ipv6_addr` value is not a prefix: it supplies only the
minimum address and causes the packet's low 64 bits to be lost.

The fixed rule intent is:

```text
prerouting:
    match iifname WANInterface
    translate destination prefix through dnat_prefixes

postrouting:
    match oifname WANInterface
    translate source prefix through snat_prefixes
```

The lookup writes the target interval minimum and maximum into IPv6 address
registers 1 and 2. The NAT expression sets `Prefix`, `RegAddrMin = 1`, and
`RegAddrMax = 2`. `RegProtoMin` is not used; it represents a transport-protocol
field rather than an IPv6 prefix length. This is equivalent to nft syntax such
as:

```nft
snat ip6 prefix to ip6 saddr map {
    fdff:a887:86e4:10::/64 : 2001:db8:1234:5610::/64
}
```

Only the high 64 bits change. The low 64 bits are preserved in both directions. The managed table has no separate schema or cross-version compatibility marker; a release describes only its current desired model and encoding.

This stage describes what must exist. It does not query current nftables state, create netlink messages, or submit anything.

## 13. Stage 6: fingerprint the desired state

```go
func FingerprintDesiredSpec(
    desired DesiredSpec,
) [32]byte
```

The fingerprint must cover every value represented by the desired model:

- Family and table name.
- WAN interface.
- Delegated prefix.
- Chain names and properties.
- Map names, types, properties, and elements.
- Rule templates and deterministic order.
- Mapping names, inside prefixes, outside prefixes, and subnet IDs.
- Operational address names, mapping references, and suffixes.

Use a canonical encoding with explicit field order, fixed integer widths, canonical `netip.Prefix` strings or bytes, and deterministic slice ordering. Do not hash Go map iteration order or library object pointer identity.

The result is attached to managed object metadata:

```text
v6pfxnatd:<fingerprint>
```

Conceptually:

```go
type DesiredArtifact struct {
    Spec        DesiredSpec
    Fingerprint [32]byte
}
```

Fingerprinting is pure and cannot modify nftables.

## 14. Stage 7: read the current managed table

```go
func ReadCurrentTable(
    tableName string,
) (CurrentTable, error)
```

Use a query-only nftables connection. List tables explicitly with `nftables.TableFamilyIPv6`, for example `ListTablesOfFamily(nftables.TableFamilyIPv6)`, then find the configured name in that IPv6 table list. Never use a family-defaulting lookup.

Read only the comparison data needed from that table:

- The configured `ip6` table.
- The comments on the managed `dnat_prefixes` and `snat_prefixes` maps.

Represent only data needed for comparison:

```go
type CurrentTable struct {
    Exists bool

    DNATComment string
    SNATComment string
}
```

A missing table is not an error:

```go
CurrentTable{Exists: false}
```

The comparison stage will require creation. If the table exists, read only the managed map comments. Objects outside this fingerprint check are not interpreted, repaired, or treated as application responsibility.

This stage does not decide whether replacement is needed and does not repair, delete, or create anything. A table lookup or managed-comment query error ends the current reconciliation and leaves the table unchanged.

## 15. Stage 8: determine whether the table is current

```go
func IsCurrent(
    current CurrentTable,
    desired DesiredArtifact,
) bool
```

`ReadCurrentTable` has already limited the lookup to the configured IPv6 table. Return `true` only when that table exists and both managed map comments exactly match the desired fingerprint. Comparing only the delegated-prefix string is insufficient.

The comparison includes:

- Table existence.
- The `dnat_prefixes` comment.
- The `snat_prefixes` comment.
- Fingerprint metadata encoded in those comments.

The first version does not need a general nftables expression decompiler or complete object inventory. The managed map comments retain:

```text
v6pfxnatd:<fingerprint>
```

If all checks pass:

```go
if IsCurrent(current, desired) {
    return nil
}
```

Return without creating a modifying connection or calling `Flush()`.

Missing managed comments or a different fingerprint return `false`. External changes that preserve the current managed map comments are outside this application's detection and recovery contract.

`IsCurrent` is pure and never modifies the kernel.

## 16. Stage 9: build the replacement transaction

```go
func BuildReplacement(
    conn *nftables.Conn,
    current CurrentTable,
    desired DesiredArtifact,
) error
```

### Preconditions

The caller has already established that:

```text
DesiredSpec was built
the fingerprint was calculated
CurrentTable was read successfully
IsCurrent returned false
```

### Connection ownership

The caller creates a fresh connection used only for this replacement and passes it to `BuildReplacement`:

```go
conn, err := nftables.New()
if err != nil {
    return fmt.Errorf("create nftables connection: %w", err)
}

if err := BuildReplacement(
    conn,
    current,
    desired,
); err != nil {
    return err
}
```

Do not reuse the query connection. It may have unrelated cached commands or lifecycle state.

`BuildReplacement` does not create, close, own, or submit the connection.

### Buffered operations

If the current table exists, queue deletion of exactly the configured IPv6 table:

```go
conn.DelTable(&nftables.Table{
    Family: nftables.TableFamilyIPv6,
    Name:   desired.Spec.TableName,
})
```

The deletion target must not be taken from an unverified family-defaulting lookup or from user-configurable family data.

Then queue, in the same connection:

1. Creation of the new `ip6` table.
2. Creation of `prerouting` and `postrouting` chains with complete properties.
3. Creation of `dnat_prefixes` and `snat_prefixes` maps with 16-byte IPv6 keys and 32-byte interval values.
4. Creation of every map element with inclusive target-prefix minimum and maximum addresses.
5. Creation of every managed rule.
6. Fingerprint comments on the managed maps.

No operation has reached the kernel yet.

### Output and failure contract

The stage neither creates the connection nor submits the transaction. On success, all replacement commands are buffered in the caller-provided connection:

```go
return nil
```

If the nftables library cannot queue a map and its elements, return that error. The caller must not call `Flush()` on a partially constructed connection. The kernel ruleset remains unchanged.

## 17. Stage 10: commit atomically

```go
func CommitReplacement(conn *nftables.Conn) error
```

The connection already contains the complete ordered replacement:

```text
delete old table if it exists
create new table
create chains
create maps
add map elements
create rules
```

Commit exactly once:

```go
return conn.Flush()
```

### Success

A successful transaction means the old managed table has been replaced by the complete new table. The daemon does not save the new PD, fingerprint, generation, update time, or table handle as applied state. The next event reads kernel state again.

Success means only that the nftables ruleset was replaced. The daemon does not inspect, clean, or migrate existing conntrack flows. Whether an existing flow continues to use its previous NAT binding is determined by the kernel conntrack lifecycle and is not part of the commit success condition.

### Failure

Return the `Flush()` error. Do not:

- Delete or create a second time.
- Attempt manual rollback.
- Downgrade to sequential commits.
- Retry immediately or on a timer.
- Write or remove the operational snapshot on this failure path.

The transaction cannot expose a partially constructed table. However, a `Flush()` error does not always prove that kernel state remained at its pre-commit value: a request may have reached the kernel even if userspace failed to receive or process its acknowledgement. Return the error, perform no second mutation, and make no applied-state claim. A later reconciliation, if triggered by another relevant route event or an administrator restart, reads the table again from the kernel.

This is the only stage allowed to modify nftables.

## 18. Publish operational status

The snapshot contents may be calculated before submission, but filesystem I/O occurs only after `CommitReplacement` returns success. Then best-effort atomically replace:

```text
/run/v6pfxnatd/status
```

The snapshot contains only deterministic line records:

```text
mapping fdff:a887:86e4:10::/64 2001:db8:1234:5610::/64
mapping fdff:a887:86e4:20::/64 2001:db8:1234:5620::/64
address gateway-c 2001:db8:1234:5620::c
```

Each `mapping` line exposes one inside/public `/64` pair. Each `address` line combines the public prefix selected by `addresses[].mapping` with the configured low-64-bit suffix. The file contains no version, state, generation, timestamp, fingerprint, or error record.

Write a mode-`0444` temporary file in `/run/v6pfxnatd`, close it, then rename it over `status`. systemd creates the directory with mode `0755` and preserves it across service restarts. The daemon never reads the file.

The unchanged path, every pre-commit failure, a `Flush()` failure, and missing or ambiguous PD state leave the previous snapshot untouched. A snapshot write failure is logged as a warning and does not turn the already successful nftables replacement into a failed reconciliation.

## 19. Reconcile function

```go
func Reconcile(
    ctx context.Context,
    cfg NormalizedConfig,
) error {
    snapshot, err := ReadNetworkSnapshot(cfg)
    if err != nil {
        return fmt.Errorf("read network snapshot: %w", err)
    }

    delegatedPrefix, err := SelectDelegatedPrefix(snapshot)
    if err != nil {
        return fmt.Errorf("select delegated prefix: %w", err)
    }

    mappings := DeriveMappings(cfg, delegatedPrefix)

    desiredSpec := BuildDesiredSpec(
        cfg,
        delegatedPrefix,
        mappings,
    )

    desired := DesiredArtifact{
        Spec:        desiredSpec,
        Fingerprint: FingerprintDesiredSpec(desiredSpec),
    }

    current, err := ReadCurrentTable(cfg.NFTTableName)
    if err != nil {
        return fmt.Errorf("read current nftables table: %w", err)
    }

    if IsCurrent(current, desired) {
        return nil
    }

    status := BuildOperationalStatus(cfg, mappings)

    conn, err := nftables.New()
    if err != nil {
        return fmt.Errorf("create nftables connection: %w", err)
    }

    if err := BuildReplacement(conn, current, desired); err != nil {
        return fmt.Errorf("build nftables replacement: %w", err)
    }

    if err := CommitReplacement(conn); err != nil {
        return fmt.Errorf("commit nftables replacement: %w", err)
    }

    if err := WriteOperationalStatus(operationalStatusPath, status); err != nil {
        slog.Warn("write operational status failed", "error", err)
    }

    return nil
}
```

The straight-line shape is intentional. Every error before `CommitReplacement` prevents submission. There is no rollback branch, retry branch, or saved-state update. Status publication follows only a successful commit and cannot change its result.

## 20. Event loop

```go
func Run(
    ctx context.Context,
    cfg NormalizedConfig,
) error {
    triggers := make(chan struct{}, 1)
    watcherReady := make(chan struct{})
    watcherDone := make(chan error, 1)

    go func() {
        watcherDone <- WatchIPv6Routes(
            ctx,
            cfg,
            watcherReady,
            triggers,
        )
    }()

    select {
    case <-watcherReady:
    case err := <-watcherDone:
        if ctx.Err() != nil {
            return nil
        }
        if err != nil {
            return fmt.Errorf("route watcher: %w", err)
        }
        return errors.New("route watcher stopped before readiness")
    case <-ctx.Done():
        return nil
    }

    Trigger(triggers)

    for {
        select {
        case <-ctx.Done():
            return nil

        case err := <-watcherDone:
            if ctx.Err() != nil {
                return nil
            }
            if err != nil {
                return fmt.Errorf("route watcher: %w", err)
            }
            return errors.New("route watcher stopped")

        case <-triggers:
            if err := Reconcile(ctx, cfg); err != nil {
                slog.Error(
                    "reconcile failed",
                    "error", err,
                )
            }
        }
    }
}
```

Runtime behavior:

```text
startup:
    run one Reconcile

relevant RTM_NEWROUTE:
    run one Reconcile

relevant RTM_DELROUTE:
    run one Reconcile

Reconcile failure:
    log the error
    do not replace the table
    continue watching
    do not actively retry
    do not promise convergence without another event

route watcher failure:
    exit the process
    let systemd apply the service restart policy
```

## 21. Function boundaries

```go
func LoadConfig(...) (NormalizedConfig, error)
func ValidateConfig(cfg Config) error
func NormalizeConfig(cfg Config) NormalizedConfig

func Run(
    ctx context.Context,
    cfg NormalizedConfig,
) error

func WatchIPv6Routes(
    ctx context.Context,
    cfg NormalizedConfig,
    ready chan<- struct{},
    triggers chan<- struct{},
) error

func Trigger(triggers chan<- struct{})

func Reconcile(
    ctx context.Context,
    cfg NormalizedConfig,
) error

func ReadNetworkSnapshot(
    cfg NormalizedConfig,
) (NetworkSnapshot, error)

func SelectDelegatedPrefix(
    snapshot NetworkSnapshot,
) (netip.Prefix, error)

func DeriveMappings(
    cfg NormalizedConfig,
    delegatedPrefix netip.Prefix,
) []PrefixMapping

func BuildDesiredSpec(
    cfg NormalizedConfig,
    delegatedPrefix netip.Prefix,
    mappings []PrefixMapping,
) DesiredSpec

func FingerprintDesiredSpec(
    desired DesiredSpec,
) [32]byte

func ReadCurrentTable(
    tableName string,
) (CurrentTable, error)

func IsCurrent(
    current CurrentTable,
    desired DesiredArtifact,
) bool

func BuildReplacement(
    conn *nftables.Conn,
    current CurrentTable,
    desired DesiredArtifact,
) error

func CommitReplacement(conn *nftables.Conn) error
```

## 22. Stage summary

| Stage | Reads | Produces | Failure effect |
| --- | --- | --- | --- |
| Load configuration | TOML file | `NormalizedConfig` | Process exits; no kernel access |
| Watch routes | rtnetlink route events | Coalesced trigger | Watcher failure exits process |
| Read network snapshot | WAN link and IPv6 routes | `NetworkSnapshot` | Managed table unchanged |
| Select PD | Filtered PD candidates | One delegated prefix | Managed table unchanged |
| Derive prefixes | PD and subnet IDs | `[]PrefixMapping` | Managed table unchanged |
| Build desired state | Config, snapshot, mappings | `DesiredSpec` | Managed table unchanged |
| Fingerprint | `DesiredSpec` | `DesiredArtifact` | Managed table unchanged |
| Read current table | nftables kernel state | `CurrentTable` | Managed table unchanged |
| Compare | Current and desired | Current or changed decision | Current path returns without commit |
| Build replacement | Desired state | Buffered caller-owned connection | No `Flush()`; managed table unchanged |
| Commit | Complete buffered connection | Success or error | One atomic transaction only |
| Publish status | Successful commit, mappings, configured addresses | Atomic operational snapshot | Warning only; previous snapshot remains on failure |

## 23. Stateless boundary

The daemon may temporarily hold these values during one reconciliation:

- Function arguments.
- The current network snapshot.
- The selected delegated prefix.
- Derived mappings.
- The desired model and fingerprint.
- The current nftables query result.
- The pending replacement transaction.

It does not retain cross-reconciliation state:

```text
no last PD
no last fingerprint
no applied state
no generation
no applied-state file
no lock file
no temporary rule file
no PID file
```

The fingerprint stored in managed nftables object metadata remains kernel state, not process-local state. `/run/v6pfxnatd/status` is output-only and is never consulted by reconciliation.

This process-stateless design does not imply stateless packet translation. nftables NAT uses conntrack, and existing NAT bindings remain outside daemon ownership.

## 24. Ownership assumptions

The daemon exclusively owns the configured table, for example:

```text
table ip6 v6pfxnat_wan1
```

Other firewall managers must not:

- Add rules to this table.
- Modify its chains.
- Modify its maps or map elements.
- Preserve daemon-generated metadata while replacing rule expressions.
- Concurrently manage another table with the same name.

The first version does not defend against malicious or concurrent privileged modification.

If another manager changes or pollutes the table, behavior is outside this application's responsibility unless the managed map comments no longer match the desired fingerprint. External modifications that preserve the current fingerprint may be treated as current.

The daemon never scans or modifies unrelated nftables tables.

## 25. Defined behavior scenarios

### A new delegated prefix appears

```text
receive RTM_NEWROUTE
    -> read the complete IPv6 route snapshot
    -> select the new /56
    -> derive new public /64 prefixes
    -> build the desired table
    -> compare managed map fingerprint comments
    -> detect a difference
    -> buffer old-table deletion and complete new-table creation
    -> call Flush() once
```

### The delegated prefix is unchanged

```text
receive a relevant route event
    -> read current state
    -> select the same /56
    -> derive the same public /64 prefixes
    -> build the same desired table
    -> prove current managed map fingerprint comments match
    -> return nil
    -> do not call Flush()
```

### Configuration changes while the prefix remains the same

```text
restart the process
    -> read the same /56
    -> build a different DesiredSpec from the new config
    -> observe a different fingerprint
    -> atomically replace the table
```

### Route state is temporarily incomplete

```text
receive RTM_DELROUTE
    -> fail to find one PD or matching default route
    -> return an error
    -> do not create a transaction
    -> leave the old table unchanged
    -> do not actively retry
    -> wait for another route event or administrator action
```

### The current nftables table cannot be queried

```text
current-table query fails
    -> return an error
    -> do not guess current state
    -> do not replace the table
    -> do not actively retry
    -> wait for another route event or administrator action
```

### Atomic submission fails

```text
Flush() returns an error
    -> return the error
    -> do not attempt a second operation
    -> do not actively retry
    -> wait for another route event or administrator action
```

## 26. Installation and operation

GoReleaser is configured to publish Linux `amd64` and `arm64` binaries and Debian and RPM packages.

Packages install:

```text
/usr/sbin/v6pfxnatd
/etc/v6pfxnatd/config.toml
/lib/systemd/system/v6pfxnatd.service        # Debian
/usr/lib/systemd/system/v6pfxnatd.service    # RPM
/usr/share/doc/v6pfxnatd/README.md
```

The config file is packaged as `config|noreplace` with mode `0600`, so package upgrades do not overwrite administrator changes.

Build from source:

```bash
go test ./...
go build -o v6pfxnatd .
```

After installing a package, edit the config and enable the service:

```bash
systemctl enable --now v6pfxnatd.service
systemctl status v6pfxnatd.service
journalctl -u v6pfxnatd.service
```

Run directly:

```bash
v6pfxnatd -c /etc/v6pfxnatd/config.toml
v6pfxnatd --version
```

The daemon reads rtnetlink and modifies nftables. The packaged systemd service runs as root while restricting the capability set to `CAP_NET_ADMIN`.

systemd creates `/run/v6pfxnatd` with mode `0755` and preserves it across service restarts. The daemon creates no PID, lock, applied-state, or temporary rules file; it only atomically publishes the mode-`0444` operational `status` snapshot after a successful nftables replacement.

`network-online.target` provides startup ordering only; it is not a delegated-prefix readiness mechanism. The daemon first establishes the route subscription and then enqueues an initial reconciliation. If the default route or PD is not ready, that reconciliation logs an error and preserves the old table. A later relevant route event retriggers a complete read.

## 27. Logging contract

Startup/configuration errors are written to stderr. Structured service logs are written to stdout and collected by systemd journal. The recommended default is `info` level with text formatting.

Log only operationally useful events at normal levels:

- Service startup and shutdown.
- Route watcher readiness or failure.
- Reconciliation stage failures with wrapped error context.
- Successful replacement after a detected change.
- Atomic submission failure.

Ignore unrelated high-frequency route events. When the table is already current, emit no `info` record; an optional `debug` record is sufficient.

Errors retain stage context:

```text
reconcile failed: select delegated prefix: expected exactly one delegated prefix candidate, got 0
reconcile failed: select delegated prefix: expected exactly one candidate, got 2
reconcile failed: commit nftables replacement: operation not permitted
```

## 28. Current implementation status

The documented pipeline is implemented:

- Strict TOML decoding, validation, and deterministic normalization.
- Long-running rtnetlink route subscription with readiness signaling, event filtering, and coalesced triggers.
- Fresh WAN/default-route/PD snapshots and strict single-PD selection.
- Pure `/64` derivation and canonical SHA-256 fingerprinting.
- Prefix-map interval encoding that preserves the low 64 bits in both translation directions.
- Managed-table current check based on the `dnat_prefixes` and `snat_prefixes` fingerprint comments.
- A no-op unchanged path and one caller-owned atomic replacement transaction when changed.
- Best-effort atomic operational status publication after successful replacement, including configured public address calculations.
- Behavior tests for CLI/configuration, derivation, fingerprinting, comparison, watcher lifecycle, trigger coalescing, nftables encoding, and status publication.

The implementation does not invoke `ip` or `nft`, persist or read applied state, actively retry reconciliation, or mutate conntrack state.

## 29. Release policy

The repository follows the same GoReleaser policy as the reference projects:

- A pushed `v*` tag creates a GitHub release.
- A manual release workflow is available.
- Linux `amd64` and `arm64` static binaries are built.
- Debian and RPM packages are generated.
- `go mod tidy` and `go test ./...` run before release.
- One `checksums.txt` file covers release artifacts.

The current release version is `0.2.2`. Production readiness still depends on validation against the target gateway kernel and nftables version.

## 30. Design summary

The complete application remains one directional, process-stateless pipeline:

```text
rtnetlink IPv6 route event
    -> read current kernel network state
    -> select exactly one DHCPv6-PD
    -> derive public /64 mappings
    -> build the complete desired table
    -> fingerprint the desired state
    -> read the current managed table
    -> compare managed map fingerprint comments
    -> unchanged: return without Flush()
    -> changed: build one complete replacement transaction
    -> call Flush() once
    -> on success: best-effort atomically publish operational status
```

The defining constraints are:

```text
do not persist or read applied state
do not invoke external commands
do not mutate nftables before the final commit
do not compensate or actively retry after failure
do not manage existing conntrack flows
do not submit when the ruleset is unchanged
replace only after every read, calculation, validation, and comparison succeeds
```

The daemon guarantees the documented success and natural failure paths. It does not guarantee that unmodeled abnormal states recover or converge automatically.
