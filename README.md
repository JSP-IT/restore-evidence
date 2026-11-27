# Restore Evidence

[![CI](https://github.com/JSP-IT/restore-evidence/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/JSP-IT/restore-evidence/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

`restore-evidence` records the results of a backup restore test and checks the
restored files against a manifest you supply. It produces a deterministic JSON
report for tooling and a self-contained HTML report for people.

You run the restore yourself, with whatever tool you use. Afterwards you point
`restore-evidence` at a manifest describing what you expected and, optionally,
at the restored directory. It reports each check as one of three statuses:

| Status | Meaning |
| --- | --- |
| `verified` | The tool looked at the restored data and it matched the manifest. |
| `failed` | The tool looked at the restored data and it did not match. |
| `not_performed` | The tool did not make this check, and the report says why. |

Results you observed yourself, such as "the service started", go in the
manifest as **supplied results**. They are copied into the report as reported
by you and are never presented as verified.

What it never does:

- start a backup or a restore, or connect to any system;
- run commands, including any `command` text written in a manifest;
- write to, rename or delete anything in the restored directory;
- follow symbolic links, or read anything outside the restored directory;
- overwrite an existing file unless you pass `-force`;
- put file contents into a report.

A report is a record of specific checks. It does not show that a backup is
recoverable in general, it is not a compliance certificate, and it is not
tamper-evident. See [Evidence limitations](docs/evidence-limitations.md).

## Contents

- [Install](#install)
- [Quick start](#quick-start)
- [Usage](#usage)
- [What is checked](#what-is-checked)
- [Privacy and redaction](#privacy-and-redaction)
- [Security model](#security-model)
- [Documentation](#documentation)
- [Development](#development)
- [License](#license)

## Install

**Prebuilt binary.** Download the archive for your platform (Linux, macOS or
Windows; amd64 or arm64) from
[Releases](https://github.com/JSP-IT/restore-evidence/releases), check it
against `checksums.txt`, extract it, and put `restore-evidence` on your `PATH`:

```sh
sha256sum --ignore-missing -c checksums.txt          # Linux
shasum -a 256 --ignore-missing -c checksums.txt      # macOS
```

```powershell
# Windows: compare the output with the line for this file in checksums.txt
(Get-FileHash .\restore-evidence_v0.1.0_windows_amd64.zip -Algorithm SHA256).Hash.ToLower()
```

A checksum confirms the download matches the published `checksums.txt`. Both
come from the same release page, so it detects corruption, not a compromised
release.

**With Go 1.26 or newer:**

```sh
go install github.com/JSP-IT/restore-evidence/cmd/restore-evidence@v0.1.0
```

Check the installation with `restore-evidence version`.

## Quick start

The repository includes a synthetic restore: a [restored directory](examples/restored),
a [manifest that matches it](examples/manifest.json), and a
[manifest that does not](examples/manifest-drift.json). All data in them is
made up.

```sh
restore-evidence verify \
  -manifest examples/manifest.json \
  -restored examples/restored \
  -json report.json -html report.html
```

```text
Restore test RT-EXAMPLE-001: pass
Checks: 15 verified, 0 failed, 0 not performed
Supplied results (not verified by this tool): 2 pass, 0 fail, 0 not performed
```

The drifted manifest shows failures and checks that were not performed:

```sh
restore-evidence verify -manifest examples/manifest-drift.json -restored examples/restored
```

```text
Restore test RT-EXAMPLE-002: fail
Checks: 11 verified, 4 failed, 3 not performed
Supplied results (not verified by this tool): 0 pass, 1 fail, 1 not performed
failed file_count (expected 5, observed 4)
failed total_bytes (expected 648, observed 398)
failed presence db/invoices.csv (expected regular file, observed not found)
not_performed size db/invoices.csv (expected 250, observed -): the file was not found
not_performed sha256 db/invoices.csv (expected 0000…0000, observed -): the file was not found
failed sha256 db/orders.csv (expected 1111…1111, observed 84f9…30e8)
not_performed sha256 logs/restore.log: the manifest gives no SHA-256 digest for this file
supplied fail service-start
supplied not_performed login-test
```

(Digests shortened here.) The full reports for both runs are in
[examples/expected](examples/expected). A test regenerates them on every CI run
and fails if a single byte changes.

## Usage

```text
restore-evidence validate -manifest FILE
restore-evidence verify   -manifest FILE [-restored DIR] [-json FILE|-] [-html FILE|-]
                          [-no-hash] [-redact-paths] [-force]
restore-evidence version
```

`validate` checks a manifest and prints its SHA-256 digest. It does not look at
any restored data.

`verify` flags:

| Flag | Effect |
| --- | --- |
| `-manifest FILE` | The manifest (required). See [Manifest format](docs/manifest.md). |
| `-restored DIR` | The restored directory to inspect, read-only. Without it, every check that needs it is reported as `not_performed`. |
| `-json FILE` | Write the JSON report to a new file, or `-` for standard output. |
| `-html FILE` | Write the HTML report to a new file, or `-` for standard output. |
| `-no-hash` | Skip SHA-256 hashing. Digest checks become `not_performed`. |
| `-redact-paths` | Replace every file path in all outputs with a label such as `[redacted file #3]`, and leave out the names of unlisted files. |
| `-force` | Allow replacing existing report files. Directories, symbolic links and the manifest itself are never replaced. |

A short text summary is always printed. It goes to standard output, or to
standard error when a report is written to standard output.

Exit codes for `verify`:

| Code | Outcome |
| --- | --- |
| `0` | `pass`: every check was performed and verified, and every supplied result was reported as pass. |
| `1` | `fail`: at least one check failed, or a supplied result was reported as fail. |
| `3` | `incomplete`: nothing failed, but at least one check was not performed or a supplied result was reported as not performed. |
| `2` | Usage error, invalid manifest, unreadable restored directory, or a report could not be written. |

`validate` exits with `0` for a valid manifest and `2` otherwise.

## What is checked

For every file in the manifest:

| Check | `verified` when | `not_performed` when |
| --- | --- | --- |
| `presence` | A regular file exists at exactly that path. | No restored directory was given, or a parent directory could not be read. |
| `size` | Its size equals the manifest's `size`. | The file is absent, or the manifest gives no size. |
| `sha256` | Its SHA-256 digest equals the manifest's `sha256`. | The file is absent, hashing is off, the manifest gives no digest, or the file could not be read. |

For the restored directory as a whole, counting regular files recursively:

| Check | Runs when | `verified` when |
| --- | --- | --- |
| `file_count` | The manifest sets `expected.file_count`. | The counts are equal. |
| `total_bytes` | The manifest sets `expected.total_bytes`. | The byte totals are equal. |
| `unlisted_files` | The manifest lists files and `allow_unlisted` is false. | No regular file exists that the manifest does not list. |

Symbolic links, devices, pipes and sockets are counted but never followed or
read. A manifest entry that turns out to be one of these fails `presence`. If
any entry in the tree cannot be read, the directory-wide checks are reported as
`not_performed` rather than guessed.

## Privacy and redaction

- Reports never contain file contents. They contain paths from the manifest,
  sizes and SHA-256 digests.
- Reports never contain the location of the manifest or the restored directory
  on your machine. The manifest is identified by the SHA-256 digest of its
  bytes. Error messages do not repeat paths either.
- Mark an entry `"sensitive": true` to replace its path with a label in every
  output. Use `-redact-paths` to do this for all entries and to leave out the
  names of unlisted files, which may also be sensitive.
- Free-text fields you write in the manifest (title, notes, supplied results)
  are copied into the report as they are. Do not put secrets in them.
- A SHA-256 digest of a short or predictable file, such as a one-line
  password file, can be confirmed by guessing. Leave the digest out for files
  like that.

## Security model

The manifest and the restored directory are treated as untrusted input:

- Manifest paths must be relative, slash-separated and clean: `..`, `.`, empty
  segments, absolute paths, drive letters, backslashes and control characters
  are rejected. See [Manifest format](docs/manifest.md#paths).
- The restored directory is opened with Go's
  [`os.Root`](https://pkg.go.dev/os#Root), so nothing can resolve outside it.
  The walk does not follow symbolic links. A file is opened for hashing only if
  the walk found it as a regular file at exactly that path.
- Manifests are limited to 64 MiB and 500,000 file entries. Unknown fields are
  rejected, so a typo cannot silently turn a check off.
- Reports are created with exclusive-create semantics, so an existing file or
  symbolic link at the target is never followed or replaced. With `-force`, an
  existing regular file is replaced atomically through a temporary file. Reports
  are never written inside the restored directory.
- The HTML report escapes every value with Go's `html/template`, contains no
  script, loads nothing, and sets a `Content-Security-Policy` that forbids
  scripts and remote content.
- The program does not import `os/exec`, `net` or `syscall`. A test enforces
  this.

The tool assumes nothing modifies the restored directory while it runs. If
something does, the report may not match what is on disk afterwards.

Please report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## Documentation

- [Manifest format and schema compatibility](docs/manifest.md)
- [Report format](docs/report.md)
- [Evidence limitations](docs/evidence-limitations.md)
- [Synthetic restore examples](docs/examples.md)
- [Licensing review](docs/licensing.md)
- [Releasing](RELEASING.md)

## Development

Go 1.26 or newer. There are no third-party dependencies.

```sh
go test ./...
go vet ./...
gofmt -l .
```

If you change report output, regenerate the expected example reports and review
the diff:

```sh
go test ./cmd/restore-evidence -run TestExampleReports -update
```

Some tests create symbolic links. On Windows they are skipped unless Developer
Mode is on or the tests run elevated. CI runs them on Linux and macOS.

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
