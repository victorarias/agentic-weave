# Hodor PR Review

This repository uses [Hodor](https://github.com/mr-karan/hodor) for advisory pull-request reviews, backed by **Vertex AI** using **Gemini 3.1 Pro Preview**.

## What runs

The workflow at `.github/workflows/hodor-review.yml`:

- triggers on non-draft pull requests (`opened`, `synchronize`, `ready_for_review`, `reopened`)
- skips forked PRs by default so Google Cloud credentials are not exposed to untrusted contributions
- clones **Hodor v0.3.4**
- applies the local patch at `.github/hodor/v0.3.4-google-vertex.patch`
- authenticates to Google Cloud from GitHub Actions
- runs Hodor with `google-vertex/gemini-3.1-pro-preview`
- posts the review back to the PR as an advisory comment

## Why a local patch is needed

Upstream Hodor `v0.3.4` supports Anthropic, OpenAI, and Bedrock model parsing out of the box, but does not yet recognize `google` / `google-vertex` provider strings during its preflight setup.

The local patch adds just enough support to:

- parse `google/...` and `google-vertex/...` model strings
- accept `GEMINI_API_KEY` for Google-hosted Gemini models
- allow Vertex AI models to use ADC-based authentication without requiring an API key

This keeps the repository on upstream Hodor while unblocking Vertex-backed reviews.

## Required GitHub configuration

This workflow now supports the same secret layout used in `victorarias/attn`.

### Minimum setup to mirror `attn`

Set these repository **secrets**:

- `VERTEX_AI_SA`
- `GOOGLE_CLOUD_PROJECT`

`GOOGLE_CLOUD_LOCATION` defaults to `global`, which matches `attn`.

### Optional newer layout

You can also use these repository **variables** instead:

- `GOOGLE_CLOUD_PROJECT`
- `GOOGLE_CLOUD_LOCATION`

Choose one authentication mode.

### Recommended: Workload Identity Federation

Set these repository **variables**:

- `GCP_WORKLOAD_IDENTITY_PROVIDER`
- `GCP_SERVICE_ACCOUNT`

The workflow uses `google-github-actions/auth@v2` with OIDC.

### Fallback: Service account key JSON

Set one of these repository **secrets**:

- `GCP_SERVICE_ACCOUNT_KEY`
- `VERTEX_AI_SA` (legacy name used by `attn`)

This should be the full JSON key for a service account with Vertex AI access.

## Review guidance loaded by Hodor

Hodor discovers repository-specific skills from `.pi/skills` and `.hodor/skills`. This repository provides:

- `.hodor/skills/hodor-review/SKILL.md`

That skill tells Hodor to focus on correctness, docs drift, Go validation, and the repository's safety and architecture constraints.

## Notes

- GitHub does not allow reading back existing secret values from another repository, so secret values cannot be copied out of `attn`; this workflow supports the same secret names instead.
- The workflow is **advisory**. Failures are summarized in the job summary and logs are uploaded as artifacts.
- The workflow currently runs only for non-fork PRs. If you later want fork coverage, use a separate hardened design with no privileged credentials in the review job.
- We intentionally removed the always-on Claude review workflow to avoid duplicate AI review bots on every PR.
- On-demand `@claude` interactions remain available via `.github/workflows/claude.yml`.
