# Approved R0 test adapters

`approved-test-adapter` fixtures are not .NET golden exchanges. They are the
bounded, product-approved exception mechanism consumed by `contractcheck`.
Each fixture must carry an `approvalId` found in
`planning/go-rewrite/contracts/approved-deviations.json`; the checker rejects
unapproved, unused, or missing evidence.

- Password recovery fixtures require a local deterministic SMTP sink and
  prefix-scoped fixture data. The run must delete those rows after capture.

No adapter may use production SMTP, real user data, production credentials, or
a hard-coded secret. A later W3 golden run must replay these fixtures against
the Go handler and retain the corresponding approval ID in its report.
