# Licensing review

Reviewed for v0.1.0.

## Project license

`restore-evidence` is licensed under the
[Apache License, Version 2.0](../LICENSE). The `LICENSE` file is the unmodified
text published at <https://www.apache.org/licenses/LICENSE-2.0.txt> (SHA-256
`cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30`).

Apache-2.0 was chosen over MIT, which other JSP-IT tools use, for its explicit
patent grant and its contribution terms (section 5). Both are permissive and
compatible with each other.

A `NOTICE` file names the copyright holder. Under section 4(d), anyone who
redistributes the software must keep that notice.

Source files carry no per-file license headers. The repository-level `LICENSE`
and `NOTICE` cover them.

## Dependencies

| Component | License | How it is used | Obligation |
| --- | --- | --- | --- |
| Go standard library | BSD-3-Clause | Compiled into every binary | Binary redistributions must reproduce the Go copyright notice and license. Release archives include it as `LICENSE-go`. |

There are no third-party modules: `go.mod` has no `require` lines and there is
no `go.sum`. If a dependency is added later, record its license here and check
that it is compatible with Apache-2.0 before merging.

## Release archives

Each archive built by the release workflow contains:

- the `restore-evidence` binary;
- `LICENSE` (Apache-2.0) and `NOTICE`;
- `LICENSE-go` (the Go project's BSD-3-Clause license, copied from the Go
  toolchain that built the binary);
- `README.md`.

## Contributions

Under section 5 of the license, contributions intentionally submitted for
inclusion are licensed under Apache-2.0 unless the contributor states
otherwise. No separate contributor agreement is required.

## Not legal advice

This review records what was checked and why. It is not legal advice. Ask
counsel if your use of the software depends on these conclusions.
