# OpenAI Build Week: v6pfxnatd

## Project summary

`v6pfxnatd` is a small Go daemon for Linux gateways whose public IPv6 prefix changes through DHCPv6 Prefix Delegation. It watches relevant IPv6 route events through rtnetlink, derives deterministic public `/64` prefixes for configured internal ULA networks, and atomically reconciles one dedicated nftables IPv6 NAT table.

The project is intentionally narrow. It does not invoke external `ip` or `nft` commands, persist applied state, manage DHCPv6, mutate conntrack state, or introduce a general firewall framework. Kernel routes and the managed nftables table remain the runtime sources of truth.

## Contribution statement

This was a human-directed, AI-assisted engineering project. The human retained product ownership, architecture authority, risk decisions, and release approval. OpenAI Codex performed most direct repository editing and command execution under those constraints.

### What the human did

The human contributor:

- Identified the real operational problem: keeping IPv6 prefix NAT mappings current when an ISP changes a DHCPv6 delegated prefix.
- Supplied and owned the initial architecture document and required the implementation to follow it before borrowing style from other repositories.
- Defined the important behavioral constraints: an event-driven and process-stateless reconciler, fresh route reads, exactly one acceptable PD candidate, deterministic mapping, no mutation before validation, a no-op unchanged path, and one atomic nftables submission.
- Selected existing projects as style references and directed a deliberately shallow repository structure rather than a platform-style hierarchy.
- Required production source and tests to live in separate directories and required release assets to be collected under one directory.
- Chose which artifacts belong to users and which belong to maintainers: `README.md` is user-facing, while detailed architecture and review material originally remained local and outside release packages.
- Corrected unsafe or unwanted workflow assumptions. Examples include stopping premature file deletion, requiring comparison with reference repositories before changing ignore policy, declining an unnecessary pull request, and explicitly approving later removal and publishing operations.
- Repeatedly challenged speculative defensive programming. The human supplied a concrete review rubric requiring every guard, helper, error path, test, and abstraction to have a traceable purpose, then authorized removal only after side effects were checked.
- Asked design-level questions about fingerprint scope, route watcher registration, PD disappearance, overlapping old/new PDs, update latency, conntrack effects, and when mappings are actually replaced. These questions exposed and documented system boundaries rather than treating generated code as automatically correct.
- Requested the operational status snapshot feature, systemd and packaging changes, a second review, formatting, tests, version bumps, and final releases.
- Controlled permissions and approvals, including repairing the workspace ACL before implementation and granting authority for commits, tags, pushes, releases, and specifically identified deletions.

### What OpenAI Codex did

Under the human's direction, Codex:

- Read the architecture and reference repositories, restated the intended behavior, and turned the requirements into implementation stages and testable invariants.
- Implemented the Go command, configuration parsing and normalization, route watcher, network snapshot reader, prefix derivation, desired-state fingerprinting, nftables comparison and atomic replacement, CLI behavior, platform boundaries, and operational status publication.
- Wrote and maintained behavior-focused tests covering configuration, daemon lifecycle, trigger coalescing, prefix derivation, fingerprints, nftables interval encoding, status output, and CLI behavior.
- Ran formatting, unit tests, vetting, Windows development builds, Linux cross-builds, Linux test compilation, GoReleaser validation, snapshot packaging, and Git diff checks.
- Reviewed its own output for concrete defects and over-engineering, then made targeted corrections after human review.
- Diagnosed a Linux-only `nltest` acknowledgement callback failure in GitHub Actions, corrected the test harness, reran the release workflow, and verified the resulting release assets.
- Reorganized the repository into `cmd/`, `app/`, `tests/`, and `release/`; consolidated release workflows; updated packaging and systemd material; and kept the layout intentionally one layer deep.
- Fixed IPv6 prefix map encoding so nftables receives both endpoints of each target `/64` interval and preserves the low 64 bits during translation.
- Added the best-effort, atomically replaced `/run/v6pfxnatd/status` snapshot only after a successful changed-table commit.
- Removed redundant internal validation, intermediate models, cleanup complexity, duplicate tests, obsolete packaging files, and ineffective build flags after the human requested a purpose-based defensive-programming audit.
- Executed the approved Git operations: commits, annotated version tags, pushes, GitHub Actions verification, and release-asset checks.
- Maintained and synchronized the detailed architecture and review documents as the design evolved.

Codex did not independently choose the product, expand its scope, or own the final tradeoffs. It proposed options and executed work; the human accepted, rejected, or refined those options.

## How AI tools were used

Codex was used as an agent inside the local development workspace, not only as a text-completion tool. Its access was scoped and auditable:

1. **Read and model the problem.** The human supplied the architecture and named reference repositories. Codex inspected them and reported its understanding before implementation.
2. **Implement against explicit contracts.** The human requested the full program and black-box/behavior tests. Codex edited repository files and continuously mapped changes back to pipeline invariants.
3. **Run the engineering loop.** Codex invoked formatters, compilers, tests, vet, release tooling, Git, and GitHub workflow inspection. Failures were diagnosed from their actual output.
4. **Use dialogue as review.** The human questioned timing, route lifecycle, fingerprint inputs, conntrack behavior, error semantics, and unnecessary defenses. Codex inspected the code before answering, and the answers became design clarification or follow-up changes.
5. **Keep authority with the human.** Read-only inspection preceded the large repository migration. Destructive cleanup, history changes, publishing, and releases occurred only after explicit human direction.
6. **Preserve review evidence.** Production history contains compact release commits, while this branch tracks the internal architecture, engineering rules, review rubric, and this contribution record for Build Week evaluation.

This workflow made the AI useful for high-volume implementation and verification while the human supplied intent, taste, constraints, skepticism, and release authority.

## Development timeline

The visible Git history is intentionally compact and linear:

| Time (Europe/Berlin) | Version | Commit | Result |
| --- | --- | --- | --- |
| 2026-07-15 19:49 | `v0.1.0` | `1e72ded` | Initial implementation, tests, packaging, service definition, and release workflow. |
| 2026-07-15 20:52 | `v0.2.0` | `5319795` | Shallow repository reorganization, consolidated release materials, test separation, licensing, and release-policy alignment. |
| 2026-07-15 22:35 | `v0.2.1` | `5dd200b` | Corrected IPv6 prefix NAT map encoding to preserve host/interface identifier bits. |
| 2026-07-16 16:52 | `v0.2.2` | `91c9669` | Added the post-commit operational status snapshot and completed the purpose-based simplification pass. |

The first release is a single large commit because the architecture and initial implementation were developed in the Codex task before the public release history was finalized. The four production commits span about 21 hours; the richer decision record is in the architecture documents and Codex task history.

## Important human/AI review examples

### Repository and documentation policy

Codex initially treated documentation cleanup too aggressively. The human stopped that path and required comparison with the other repositories' `.gitignore`, local exclude, changelog, workflow, and GoReleaser policies. The result was a deliberate distinction:

- keep detailed internal documents locally;
- do not ship them in production packages;
- keep `README.md` focused on operators;
- track the internal documents only on this reviewer branch.

### Prefix translation correctness

Review of the nftables map representation established that a prefix-NAT verdict requires an inclusive address range, not a single 16-byte address. The corrected map values provide the minimum and maximum address of the target `/64`, and the NAT expression consumes both registers. This preserves the packet's low 64 bits.

### Failure behavior

The human explicitly questioned what happens when a PD disappears or when old and new PDs overlap. The resulting documented contract is conservative:

- zero or multiple acceptable PD candidates cause reconciliation to fail without changing the current table;
- a later relevant route event can trigger a fresh read;
- there is no timer retry or one-second SLA;
- rule replacement is atomic, but existing conntrack flows are not migrated;
- the daemon watches route state changes, not DHCP lease timers directly.

### Removing generated-code excess

The human supplied a strict purpose test for defensive programming. Codex audited each extra check and abstraction, proposed only concrete removals, implemented the approved set, and reran tests and release checks. This removed duplicate validation and tests without weakening public boundaries or atomic commit behavior.

## Verification performed

Across the implementation and release tasks, the following checks were reported as passing:

- `gofmt`
- `go test ./...`
- `go vet ./...`
- native development build
- Linux `amd64` cross-build
- Linux test-binary compilation
- GoReleaser configuration validation
- GoReleaser snapshot packaging for Linux `amd64` and `arm64`
- Debian and RPM artifact generation
- Git diff whitespace checks
- GitHub Actions release workflows
- release-asset inspection for archives, packages, and checksums

The remaining production caveat is explicit: final readiness depends on validation against the target gateway's kernel, routing behavior, nftables version, and DHCPv6 client behavior.

## Reviewer guide

For a focused evaluation:

1. Read this document for authorship and process context.
2. Read `architecture.md` sections 2, 3, 6, 8, 15, 17, and 28 for the main invariants and implemented pipeline.
3. Read `engineering-principles.md` for deliberate scope limits.
4. Read `REVIEW.md` for the exact criteria used to accept or reject findings.
5. Inspect `app/daemon.go`, `app/routes_linux.go`, `app/model.go`, `app/nft_linux.go`, and `app/status_linux.go` in pipeline order.
6. Inspect `tests/` for behavior-level coverage.
7. Compare the production tags `v0.1.0` through `v0.2.2` to see the compact release timeline.

No runtime source, test, release configuration, or packaging behavior is changed by the reviewer documentation commit.
