# Synthetic restore examples

Everything in [`examples/`](../examples) is synthetic: the "restored" files are
small made-up CSV, INI and log files, and no real backup or system was
involved. Use them to try the tool and as templates.

```text
examples/
  restored/                 a pretend restored directory (4 files, 398 bytes)
  manifest.json             matches restored/ exactly
  manifest-drift.json       deliberately disagrees with restored/
  expected/                 reports generated from the two manifests
```

The files under `restored/` are stored byte for byte (see `.gitattributes`), so
their digests are the same on every platform.

## 1. A passing restore test

```sh
restore-evidence verify -manifest examples/manifest.json -restored examples/restored \
  -json report.json -html report.html
echo $?    # 0
```

All 15 checks are `verified`: 3 directory-wide checks, plus presence, size and
SHA-256 for each of the 4 files. Both supplied results are reported as `pass`.
Expected output:
[report.txt](../examples/expected/report.txt),
[report.json](../examples/expected/report.json),
[report.html](../examples/expected/report.html).

## 2. A restore test with drift

```sh
restore-evidence verify -manifest examples/manifest-drift.json -restored examples/restored
echo $?    # 1
```

The drift manifest shows each kind of result:

| Manifest entry | Result |
| --- | --- |
| `expected.file_count: 5` | `failed`: 4 files were found. |
| `expected.total_bytes: 648` | `failed`: 398 bytes were found. |
| `db/invoices.csv` | `failed` presence. Its size and digest checks are `not_performed` because there is no file. |
| `db/orders.csv` with a wrong digest | `failed` sha256, showing both digests. Size is `verified`. |
| `logs/restore.log` without a digest | sha256 `not_performed`: the manifest gives no digest. |
| `db/customers.csv` with `"sensitive": true` | Verified, but shown only as `[redacted file #2]`. |
| supplied `service-start: fail` | Counted as a reported failure. |
| supplied `login-test: not_performed` | Counted as reported not performed. |

Expected output:
[report-drift.txt](../examples/expected/report-drift.txt),
[report-drift.json](../examples/expected/report-drift.json),
[report-drift.html](../examples/expected/report-drift.html).

## 3. Documenting a test without the restored data

If the restored data is no longer available, for example because the test
environment was torn down, you can still produce a report of the supplied
results:

```sh
restore-evidence verify -manifest examples/manifest.json -json report.json
echo $?    # 3
```

Every file and directory check is `not_performed` with the reason "no restored
directory was supplied", and the outcome is `incomplete`. The report never
presents these checks as passed.

## 4. Redacting paths

```sh
restore-evidence verify -manifest examples/manifest.json -restored examples/restored \
  -redact-paths -html report-redacted.html
```

Every path becomes `[redacted file #N]`, numbered in sorted path order, and
unlisted file names are left out.

## 5. A typical real workflow

1. Before the backup, or from a trusted copy of the source, record each
   file's path, size and SHA-256 in a manifest. Store it where the restore
   process cannot change it.
2. Restore the backup with your normal tooling into an isolated location.
3. Check the application yourself and add what you observed as
   `supplied_results`, with ticket or log references.
4. Run `restore-evidence validate`, then `restore-evidence verify` with
   `-restored`, writing the reports outside the restored directory.
5. Keep the manifest and both reports together. The report's `manifest.sha256`
   ties it to the manifest.
