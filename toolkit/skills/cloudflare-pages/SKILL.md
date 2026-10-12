---
name: cloudflare-pages
description: Invariants for a Cloudflare Pages project deployed by direct upload — deploys only through the project's operator CLI, no secrets on the project, thin Pages Functions, every address behind Access for a private site, three-layer crawler blocking, and the checklist for a new .env variable. Use when working on Pages Functions (functions/), public/_headers, robots.txt, the base layout's meta tags, or .env.template.
---

# Cloudflare Pages

One direct-upload Cloudflare Pages project serves the static build and the Pages Functions under
`functions/`. OpenTofu owns the project, its variables and any Access gate (`opentofu` skill);
the deploy manual says how to run things; the project's own instructions name its specifics.

1. **Deploys go through the operator CLI** (`<project>ctl deploy --env prod|preview`, a wrangler
   direct upload). A git push publishes nothing. Never `wrangler pages deploy` by hand.
2. **No secrets on the Pages project.** A plain variable goes into the OpenTofu stack for both
   environments and takes effect with the next deploy.
3. **Functions stay thin.** One file per endpoint (or one catch-all forwarder) under
   `functions/api/`, each declaring only the bindings it uses. Logic belongs in the backend where
   the project has one.
4. **A private site keeps every address behind Access** — production and every branch preview are
   in the Access application's destinations, and nothing assumes a visitor who has not signed in.
5. **Crawler blocking on a private site is three layers that change together**: `robots.txt`,
   the `X-Robots-Tag` header in `public/_headers`, and the robots meta tag in the base layout.
6. **A new `.env` variable** goes into `.env.template` with where its value comes from, into the
   operator CLI's list of keys, and into the deploy manual when the user must create it.
