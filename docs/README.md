# Reviewer Documentation

This directory is tracked on the `codex/openai-build-week` branch so reviewers can inspect the design record that was intentionally kept out of the production release branch.

Start here:

1. [`openai-build-week.md`](openai-build-week.md) — project timeline, human/AI contribution statement, and the way Codex was used.
2. [`architecture.md`](architecture.md) — complete architecture, invariants, pipeline, failure semantics, packaging, and implementation status.
3. [`engineering-principles.md`](engineering-principles.md) — scope boundaries and the rules used to prevent speculative complexity.
4. [`REVIEW.md`](REVIEW.md) — the acceptance criteria used for focused code review.

The production `master` branch keeps `docs/` out of the repository through the local Git exclude policy. This reviewer branch deliberately tracks the directory without changing runtime behavior or release packaging.
