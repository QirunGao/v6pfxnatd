# Review Acceptance Rule

`v6pfxnatd` is an event-driven nftables IPv6 prefix NAT reconciler for dynamic DHCPv6-PD. The process keeps no cross-reconcile state, while the nftables NAT behavior is stateful through conntrack. Reviews must stay inside this pipeline:

1. Read one TOML config.
2. Observe relevant IPv6 route changes.
3. Read a fresh network snapshot and select one DHCPv6-PD.
4. Derive deterministic `/64` mappings from validated configuration and route state, preserving the low 64 bits during translation.
5. Compare the desired fingerprint with the two managed map comments.
6. Atomically replace the table once, only when changed.
7. After a successful replacement, best-effort atomically publish the output-only operational snapshot.

## Accept Only These Findings

A finding is valid when it proves a direct risk under ordinary use:

1. Early mutation
   The program queues or commits an nftables mutation before all reads, calculations, validation, and comparison succeed.

2. False unchanged
   `IsCurrent` returns true when the configured IPv6 table is missing or either managed map comment does not match the current desired fingerprint.

3. Unnecessary commit
   An unchanged desired state can still call `Flush()`.

4. Non-atomic replacement
   A changed table is updated through multiple commits or can be left partially replaced.

5. Nondeterministic desired state
   Equivalent inputs can produce different ordering, fingerprints, mappings, or nftables objects.

6. Ownership escape
   A family-defaulting lookup, unverified deletion target, or config value can cause inspection or mutation outside the configured managed `ip6` table.

7. Ordinary pipeline failure
   A normal, plausible config cannot execute a documented stage because of a program bug rather than a missing route, ambiguous PD, kernel rejection, or nftables error.

8. Watcher lifecycle violation
   The initial reconciliation can run before route subscription readiness, or the watcher returns after successful subscription, misreports context cancellation as failure, or cannot pass a runtime subscription error to `Run`.

9. Connection ownership violation
   `BuildReplacement` creates or submits its own connection, or the caller can flush a partially constructed replacement after it returns an error.

10. Commit-result overclaim
    Code or documentation treats every `Flush()` error as proof that the kernel table remained unchanged, or performs a second mutation to resolve an uncertain acknowledgement result.

11. Incorrect prefix translation
    A managed map value omits either endpoint of the target `/64`, or the NAT expression does not consume both address registers, so translation can clear or otherwise change the low 64 bits.

12. Status publication boundary violation
    The daemon writes or removes status before a successful changed-table commit, refreshes it on the unchanged path, reads it as input, or turns a status write failure into reconcile failure.

## Rejected Findings

Do not accept findings that only request:

- Interface, route, PD, or kernel-feature startup preflight.
- A retry queue, backoff scheduler, or manual rollback.
- A guarantee of automatic convergence after a failed reconciliation without another route event or administrator restart.
- Conntrack cleanup, flow migration, or a guarantee that existing flows immediately use a new prefix.
- RFC 6296 NPTv6 behavior.
- Applied-state, generation, lock, PID, or temporary rule files.
- Partial table repair or preservation of foreign objects in the managed table.
- Detecting or recovering from external pollution inside the configured table when the managed map comments still contain the current fingerprint.
- Multiple-PD ranking or metric-based guessing.
- Deleting the old table when PD temporarily disappears.
- A route, DHCPv6, firewall, or conntrack management framework.
- Generic netlink wrappers, planner/executor layers, internal packages, or interfaces added only for future use.

A runtime operation is allowed to fail at its documented stage. Turning every possible kernel or network failure into earlier validation is not a correctness improvement.

## Required Shape

Every accepted finding must include:

- The exact pipeline invariant violated.
- The exact function or stage responsible.
- The smallest direct fix that preserves the single-direction pipeline.
