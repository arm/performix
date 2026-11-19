<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Technical Decision Records Process

This document outlines a basic process for creating technical decision records (TDRs) for Arm Performix. It aims to support sound technical decisions, ensure alignment across the project, and preserve decisions and their rationale for future reference.

## Process Flow

The following steps should be followed by developers when proposing an architectural change or new architectural component (see [when to create a technical proposal](#when-to-create-a-technical-proposal) below):

1. Copy the [template](template.md) to docs/tdrs/YYYY-MM-DD-concise-title.md. Add your [content](#proposal-content) and open a pull request.
2. Invite relevant team leads and architect to a review meeting, giving attendees time to read and digest the proposal. Team leads may delegate.
3. If all invited reviewers agree on the PR, the review may finish asynchronously before the meeting. Otherwise, use the meeting to resolve remaining questions.
4. After review, the proposer must incorporate agreed changes into the TDR.
5. Obtain PR approval of the final TDR from a team lead or the architect.
6. After acceptance, the proposer must merge the PR to `main` and publicise the decision to all affected parties. Record how any objections were resolved on the PR.
7. If rejected, the reasoning must be documented on the closed PR.

A TDR can be superseded by another: add a "supersedes" link to the new TDR and a "superseded by" link to the old TDR.

## Proposal Content

Each TDR is a **one-page** record of a **single** technical decision. Link to supporting analysis and detailed learnings rather than including them in the TDR.

Use a **diagram** as the main explanation of the proposed decision. Decide what reviewers need to understand, then show the change and only the context needed to explain it. Include existing functionality and implementation detail only when they help explain the decision. Check that the diagram conveys the decision and its consequences without the author having to talk reviewers through it.

TDRs must:

- Follow the template provided at [TDR template](template.md)
- List goals and constraints
- Include a high level summary of the user level features enabled
- Briefly describe alternatives and tradeoffs
- List API changes at gRPC, CLI, recipe/tool API and configuration file format boundaries
- Address testability where it affects the decision

Include enough detail for reviewers to assess the decision and its consequences. Keep implementation detail in supporting documents.

## When to create a technical proposal

Create a proposal when the decision is important enough that the reasoning should be reviewed and recorded.

A proposal is usually needed when one or more of the following are true:

- the change has cross-team impact
- it changes an interface or contract between components
- it introduces a new subsystem or architectural pattern
- it substantially affects user workflows or UX
- it has significant long-term consequences or locks us into a particular direction
- there are multiple credible options with tradeoffs
- it affects recipe APIs, gRPC contracts, data formats, deployment shapes, or security

A proposal is usually not needed for implementation details, routine refactors, low-risk improvements, or reversible local decisions.

Rule of thumb: if the decision will still matter in six months, it probably deserves a proposal or TDR entry.

## Earlier TDRs

Earlier TDRs are indexed in [Confluence](https://confluence.arm.com/spaces/ITS/pages/1937340257/Technical+Decision+Records) (Arm internal access required).
