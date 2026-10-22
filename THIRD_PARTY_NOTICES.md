# Third-party notices

Yaginx first-party source is licensed under GPLv3. The complete dependency
versions are pinned by the checked-in manifests and lock files:

- Go: `gateway/go.mod` and `gateway/go.sum`.
- Admin UI: `adminui/package.json` and `adminui/pnpm-lock.yaml`.
- Documentation site: `yaginx-docs-site/package.json` and
  `yaginx-docs-site/pnpm-lock.yaml`.

All listed packages are resolved from public Go modules, npm, or public GitHub
repositories, with one exception: the Modra UI packages (`@modra/react`,
`@modra/tokens`, `@modra/tailwind-preset`, MIT) are served read-only and
without authentication from `https://npm.feinian.net/`, configured in
`adminui/.npmrc`. Their Radix UI, class-variance-authority and lucide-react
(ISC) dependencies come from the public npm registry. Icons come from
`@feinian/fontawesome-free-react`, generated from Font Awesome Free 6.4.0
(icons CC BY 4.0, code MIT, https://fontawesome.com/license/free), served from
the same registry. Their upstream license and copyright notices remain authoritative
and must be included in generated release SBOMs. The release workflow runs the
SBOM and license checks on every tag. Fonts and other bundled assets retain the
license files shipped with their upstream package; no private asset is required
at runtime.

Direct first-party dependencies currently include the public Go modules from
`gateway/go.mod`, React, Modra UI, Tailwind CSS and Vite packages from `adminui/package.json`,
and Docusaurus packages from `yaginx-docs-site/package.json`. See the SPDX
artifact attached to each GitHub Release for the exact transitive closure.
