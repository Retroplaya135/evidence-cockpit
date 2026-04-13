<div align="center">
  <img src="docs/cover.png" alt="Evidence Cockpit" width="100%">
  <p><strong>Policy-aware ingestion for research records that must remain traceable.</strong></p>
  <p>
    <a href="#why-evidence-cockpit">Why</a> ·
    <a href="#processing-model">Processing model</a> ·
    <a href="#quick-start">Quick start</a> ·
    <a href="#verification">Verification</a> ·
    <a href="#project-boundaries">Boundaries</a>
  </p>
  <p>
    <img alt="Status: library and CLI foundation" src="https://img.shields.io/badge/status-library_%2B_CLI_foundation-64748b">
    <img alt="Go 1.22 or newer" src="https://img.shields.io/badge/go-1.22%2B-00ADD8">
    <img alt="Dependencies: standard library" src="https://img.shields.io/badge/dependencies-standard_library-0f766e">
    <img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-16a34a">
  </p>
</div>

Evidence Cockpit is a compact Go implementation for importing research records without losing where they came from or why they were accepted. It reads CSV, JSON, JSONL, and Markdown, normalizes common fields, records source and row hashes, and assigns an explicit quality state with reasons.

Evidence Cockpit is for engineers building internal research or market-analysis pipelines where collection, normalization, review, and scoring need clear boundaries. It uses only the Go standard library.

## Why Evidence Cockpit

Import tools often collapse three different decisions into one: whether a file can be parsed, whether it is allowed into a system, and whether its records are good enough for downstream use. That shortcut makes later review difficult. A clean row can still come from a disallowed path, a permitted file can contain conflicting values, and a normalized record can still be too old or incomplete to support a claim.

Evidence Cockpit keeps those decisions separate. Admission policy runs before parsing. Normalization preserves the source context instead of replacing it. Quality routing produces an explicit state and reason set. Stable hashes make repeat imports and row-level comparisons inspectable. A small scorer demonstrates how qualified evidence can be consumed without turning scoring rules into hidden ingestion policy.

Use it as a reference boundary for:

- research imports that need a review queue instead of silent acceptance;
- market or service-area datasets assembled from mixed document formats;
- internal tools that must explain why a row was accepted, held, or rejected;
- test fixtures for provenance, duplicate handling, and policy enforcement.

It is not a crawler, source-of-truth database, enrichment service, or assertion that imported material is accurate.

## Processing model

| Stage | Responsibility |
| --- | --- |
| Admission | Applies root, filename, and input-policy checks before parsing |
| Parsing | Converts supported source formats into a common record shape |
| Normalization | Handles dates, URLs, geography, money, and public contact fields |
| Lineage | Records source identity and stable hashes for the source and row |
| Quality routing | Marks records as accepted, manual review, or quarantined with reasons |
| Scoring example | Consumes qualified service-market inputs separately from ingestion |

The split matters: a parser being able to read a file does not mean the file is allowed into the pipeline, and a normalized record is not automatically ready for scoring or contact.

## Implemented capabilities

### Admission and parsing

- Root-aware path policy with separate accepted, review, and rejected locations.
- Filename checks that reject material resembling secrets or environment files.
- Readers for CSV, JSON, JSONL, and Markdown inputs.
- Stable source identity and row hashing for traceability and duplicate checks.

### Normalization and review

- Common normalization for dates, URLs, geography, monetary values, and public contact fields.
- Duplicate, conflict, freshness, and completeness checks.
- Accepted, manual-review, and quarantined outcomes with machine-readable reasons.
- Reports that retain original source information alongside normalized fields.

### Downstream boundary

- A service-market scoring example that consumes already-qualified records.
- Tests that keep policy decisions independent from scoring behavior.
- Standard-library-only packages that can be studied without a framework or service runtime.

## Quick start

Evidence Cockpit requires Go 1.22 or newer. Run the synthetic fixture from the repository root:

```bash
go run ./cmd/evidence-import --input fixtures/evidence.csv
```

The fixture produces three outcomes: one accepted record, one manual-review record, and one quarantined record. Add `--report` to print the complete JSON report, including normalized values, hashes, and routing reasons.

The CLI treats the current directory as its policy root. Its default policy admits files under `fixtures/` and `contracts/`, routes `review/` inputs to manual review, and rejects other roots. Files with names resembling environment or credential material are rejected even when their format is otherwise supported.

## Practical usage

The bundled fixture is the fastest way to inspect the full decision path. The default summary shows the outcome counts; `--report` exposes the normalized records, hashes, and reason codes used to reach them.

For an internal integration, keep source collection outside this repository and pass only approved local inputs to the importer. Treat the report as an intermediate review artifact: persist it in a system with appropriate access controls, let a human resolve manual-review records, and send only accepted records to a separate scoring or analysis stage.

When adding a format or normalizer, add tests for both valid input and the nearby failure cases. A new parser should not weaken path admission, duplicate detection, or the distinction between malformed and policy-rejected material.

## Quality routing

Routing checks cover duplicate inputs, conflicting values, record freshness, and whether the available fields are ready for downstream claims. Each result carries reasons so a caller can distinguish a parsing failure from a policy decision or an evidence-quality problem.

Hashing supports traceability and duplicate detection; it does not prove that the underlying source is true. Likewise, “accepted” means the record passed the configured checks, not that a person has independently verified every claim.

The service-market scorer is deliberately separate from ingestion. That keeps a change to a scoring rule from silently changing source admission, and it allows ingestion reports to remain useful even when a team uses a different downstream model.

## Verification

```bash
make check
make build
```

`make check` runs the test suite and `go vet`. `make build` verifies compilation without leaving a binary in the repository.

Tests cover supported formats, path policy, normalization, lineage hashes, quality routing, duplicate behavior, and the scoring example.

## Repository map

| Path | Responsibility |
| --- | --- |
| `cmd/evidence-import` | CLI entry point and report selection |
| `internal/ingest` | Admission policy, parsers, normalization, hashing, and quality routing |
| `internal/scoring` | Separate example consumer for qualified service-market evidence |
| `fixtures/evidence.csv` | Synthetic import demonstrating all three quality outcomes |
| `Makefile` | Aggregate check, build, and demo commands |
| `CONTRIBUTING.md` | Expectations for policy, parser, and normalizer changes |
| `SECURITY.md` | Sensitive-input and vulnerability reporting guidance |

## Project boundaries

This release is a library and CLI foundation. It does not include a dashboard, hosted API, persistent database, source-provider integration, or background collection. A production deployment would need its own storage, access controls, retention rules, and source-specific authorization.

Use synthetic data in fixtures, examples, and issue reports. Quality states do not authorize outreach or establish the truth of a source. See [SECURITY.md](SECURITY.md) for reporting sensitive issues and [CONTRIBUTING.md](CONTRIBUTING.md) for changes to policy or normalization behavior.

Interfaces may change while the policy model is refined. A production deployment would also need a durable schema for review decisions, migrations, tenant boundaries, retention enforcement, operator authentication, and an explicit process for correcting already-consumed evidence.

## Contributing, security, and license

Changes should preserve the distinction between admission, parsing, normalization, quality routing, and scoring. Include focused tests for both accepted and rejected paths, and keep all examples synthetic.

Report policy bypasses, provenance loss, unsafe path handling, parser denial-of-service risks, or accidental sensitive-data exposure through GitHub's private security-advisory channel.

Evidence Cockpit is available under the [MIT License](LICENSE).
