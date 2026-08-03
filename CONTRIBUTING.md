<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Contributing

Use this document to understand how to contribute to Arm Performix.

## Branches and commits

- Prefer short-lived branches with `feature/...`, `fix/...`, `test/...`, or `chore/...` prefixes.
- Before review, rebasing and force-pushing is acceptable to keep history tidy.
- After review has started, prefer additive commits so review context is preserved.

## Pull requests

Performix release notes are generated from pull requests. Pull-request titles must therefore be clear and concise, and every pull request must have the appropriate labels.

### Titles

Use the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/#summary) format with a short description of the change. Do not include Jira references such as `APAP-1234` in the title. Put the Jira issue and detailed context in the pull-request body using [`PULL_REQUEST_TEMPLATE.md`](.github/PULL_REQUEST_TEMPLATE.md).

Pull requests are squash-merged, so the pull-request title becomes the commit message on `main`. Examples include:

- `feat(recipes): add recipe filtering`
- `fix(targets): always show connection error`
- `test(e2e): stabilise recipe form flow`
- `chore(deps): bump electron`

### Labels

Every pull request needs exactly one change-type label, which determines the release notes category:

- `feature` for a change that adds or significantly changes product behaviour;
- `bugfix` for a change that fixes existing product behaviour; or
- `misc` for work that does not change the shipped product, such as tests, CI, configuration, or documentation.

Add `internal` when a product change is not yet publicly user-facing, for example because it is behind a feature flag or belongs to a larger unfinished requirement. Internal changes are omitted from public release notes.

The `gui` and `core` component labels identify the affected release surfaces and are normally applied from changed paths. Confirm them when a change crosses component boundaries or affects generated interfaces. For core releases, `feature` increments the engine minor version, `bugfix` increments the patch version, and `misc` does not increment it.

### Workflow

- Open work-in-progress pull requests as drafts and mark them ready only when they are reviewable.
- Keep each pull request scoped to one coherent change. Split unrelated or independently deliverable work.
- Choose reviewers with knowledge of the affected area where possible.
- Call out breaking changes, feature-flag decisions, generated outputs, manual testing, and automated coverage.
- Preserve and complete the pull-request template to provide the context and evidence reviewers need.
- Reviewers should use the checklist in the pull-request template.

## Testing and checks

Run the narrowest relevant checks for the files changed, then broaden coverage in proportion to the risk:

- prefer unit tests for local behaviour;
- update integration or boundary tests when interfaces or service boundaries change;
- consider E2E coverage for significant GUI workflows
- consider Robot coverage for CLI or engine flows that require a real target.

## Risky or user-facing changes

- Consider a feature flag for large, risky, or incomplete functional changes.
- Follow the component guidance before adding or changing a flag:
  - [GUI feature flags](DEVELOPMENT.md#gui-feature-flags)
  - [Core feature flags](DEVELOPMENT.md#core-feature-flags)
- New user-facing engine errors should use the message catalogue and `Message` type described under [engine messages](DEVELOPMENT.md#engine-messages).

## Copyright and License

- [REUSE](https://reuse.software/) is used to manage copyright and license compliance:
  - 'task deps:tools' installs the `reuse` tool.
  - `copyright:annotate` adds copyright and license metadata to files missing it, and `copyright:lint` checks it's correct.
- All new files generally need copyright (`SPDX-FileCopyrightText`) and license (`SPDX-License-Identifier`) notices.
- Use `.license` sidecar files adjacent to the file in question for special one-off files, for example `example.json.license`.
- Otherwise, for files that cannot easily contain comments, prefer central coverage in [`REUSE.toml`](REUSE.toml). Update `REUSE.toml` to apply shared metadata to generated files, fixtures, data files, or other paths where per-file headers are unsuitable.
- Some formats may carry their own visible metadata too, such as the `copyright` and `license` fields in the telemetry JSON files.
- The default first-party snippet is maintained in [`copyright-license-header.txt`](copyright-license-header.txt).
- Run `task copyright:annotate` to add headers, or `.license` sidecar files where headers are not supported, to first-party files; then review the diff.
- Run `task copyright:lint`; CI also runs this through the copyright/license workflow and fails on missing metadata.
- For third-party files, preserve existing copyright/license notices and do not add Arm notices.
- If third-party metadata is needed, use its actual license in an inline SPDX line, `.license` sidecar file, or `REUSE.toml`.

## Sensitive Changes

This internal repository is periodically mirrored to an open-source public repository. Pull requests containing sensitive information should be carefully reviewed before merging.

- `.ossmosis.json` is the source of truth for classifying repository paths for the public mirror. Paths are classified as public by default, while exclusion rules identify internal-only paths that must be omitted from the open-source repository. These classifications are also used when scanning content intended for publication for restricted or sensitive terms.
- If the `restricted-terms.yaml` workflow detects sensitive changes, the check will fail, but it is not configured as a required check for merging PRs. Consider whether any offending usages need fixing.
- If the change must be delivered to the internal repository despite containing sensitive information (e.g. so that a sensitive feature can be delivered):
  - Consider whether the sensitive information can be removed or replaced with a placeholder.
  - Add the list of files to the `.ossmosis.json` manifest to prevent them from being mirrored to the open-source repository.
- It is not possible to reliably detect all sensitive information automatically, so consider whether structural changes can mitigate the exposure of sensitive information, or make it easier to omit using the `.ossmosis.json` manifest.
- Consider the impact on the open-source repository if portions of the changes do not exist in the open-source repository (e.g. whether it can still be built, whether certain functionality will be missing, etc.).

## Security

Refer to [SECURITY.md](SECURITY.md) for the security policy and reporting process.
