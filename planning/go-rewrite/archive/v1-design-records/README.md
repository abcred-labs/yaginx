# Archived v1 Design Records

The files retained in this directory document the completed v1 Go-rewrite
programme. They may mention the former `gateway_schema_version` tracker,
`import-legacy`, root-level `migrate`, or .NET compatibility behavior.

They are historical evidence only. Do not use them to deploy or operate v2.
Current operations are defined by the v2 OpenAPI contract, the v2 baseline,
`yaginx database init|migrate|status`, and the deployment documentation.

Automatic upgrades, their timer/worker units, CLI controls, and release-age
policy have been removed. References to them here describe historical behavior
only; administrators now schedule manual upgrades.
