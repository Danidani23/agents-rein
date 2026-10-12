---
name: opentofu
description: OpenTofu conventions for a project's infra/ stack — the stack owns the resources and the deploy tool owns the bytes, no secret in the state, validated inputs, a why-comment on every resource, values shared with the operator CLI, who runs plan and apply. Use when writing or changing OpenTofu or Terraform files (*.tf, infra/).
---

# OpenTofu (`infra/`)

`infra/` owns the project's cloud resources. It is run only through the project's operator CLI
(`go-cli` skill), which maps `.env` to the provider's variables; the deploy manual says how.

1. **OpenTofu owns the resources, the deploy tool owns the bytes.** Nothing in `infra/` uploads
   an app; nothing in the deploy command creates a resource.
2. **No secret ever enters the state.** Provider tokens come from the environment; resources
   carry plain variables only. That is what makes a committed state file (private repo, one
   operator) acceptable.
3. **Shared account-level things are read, never managed** — a Zero Trust team other sites use,
   a zone another project owns. Only this project's own resources are resources here.
4. **An output another system trusts** (an application ID, an audience tag) says so. A plan that
   replaces its resource is followed by redeploying whatever trusts it, and the handover says so.
5. **Validate inputs** that would otherwise fail late and opaquely (`validation` blocks on ids,
   `precondition` where two things must agree).
6. **A why-comment on every resource**: what it is for and what breaks without it.
7. **Values shared with the operator CLI** (project name, production branch, output names) say
   so in their `description` and are changed on both sides in one change.
8. **Checks**: `tofu fmt` and `tofu validate` (or the project's check task) before handing over;
   `plan` is agent-run. The human runs `apply`, then commits the state where it is committed —
   one machine at a time, pull before plan.
