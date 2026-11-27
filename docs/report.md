# Report format

`restore-evidence verify` can write a JSON report (`-json`) and an HTML report
(`-html`). Both are built from the same data and are deterministic: the same
manifest, restored directory, flags and tool version produce byte-identical
output on every platform. The report contains no generation timestamp for
that reason. The test's own timestamps come from the manifest.

The HTML report is for people. Treat its layout as unstable. Tools should read
the JSON report.

## JSON report

A complete example is in
[examples/expected/report-drift.json](../examples/expected/report-drift.json).

| Field | Notes |
| --- | --- |
| `report_schema_version` | Currently `1`. |
| `tool.name`, `tool.version` | The program that produced the report. |
| `manifest.schema_version` | Version of the input manifest. |
| `manifest.sha256` | SHA-256 of the exact manifest bytes. Use it to tie a report to the manifest it was built from. |
| `manifest.file_entries` | Number of entries in `files`. |
| `test` | Copied from the manifest. |
| `inputs.restored_directory_supplied` | Whether `-restored` was given. Its location is not recorded. |
| `inputs.hashing_enabled` | `false` when `-no-hash` was given. |
| `inputs.paths_redacted` | `true` when `-redact-paths` was given. |
| `outcome` | `pass`, `fail` or `incomplete`. See below. |
| `summary` | Counts of checks by status and supplied results by outcome. |
| `checks[]` | The tool's own checks. See below. |
| `supplied_results[]` | Copied from the manifest. `reported_outcome` holds the operator's outcome. |
| `observed` | What the walk of the restored directory found. Absent when no directory was given. |
| `limitations[]` | Fixed text stating what the report does not show. |

### `checks[]`

| Field | Notes |
| --- | --- |
| `kind` | `presence`, `size`, `sha256`, `file_count`, `total_bytes` or `unlisted_files`. |
| `subject` | The manifest path, or a label such as `[redacted file #2]`. Absent for directory-wide checks. |
| `status` | `verified`, `failed` or `not_performed`. |
| `expected`, `observed` | Strings, including for numbers, so every check has the same shape. Absent when there is nothing to show. |
| `reason` | Why a check was not performed, or extra detail on a failure. Human-readable; do not parse it. |

Directory-wide checks come first, then the checks for each file in path order:
`presence`, `size`, `sha256`.

### `observed`

| Field | Notes |
| --- | --- |
| `regular_files`, `total_bytes` | Regular files under the restored directory and their total size. |
| `unlisted_files` | Regular files that the manifest does not list. |
| `unlisted_sample` | The first 20 of those, in walk (lexical) order. Left out with `-redact-paths`. |
| `symlinks_not_followed` | Symbolic links found. They are not followed. |
| `other_non_regular` | Devices, pipes, sockets and other special entries. They are not read. |
| `unreadable_entries` | Entries that could not be read. When this is not zero, directory-wide checks are `not_performed`. |

### Outcome

| Outcome | Rule |
| --- | --- |
| `fail` | Any check is `failed`, or any supplied result is `fail`. |
| `incomplete` | Otherwise, if any check is `not_performed` or any supplied result is `not_performed`. |
| `pass` | Otherwise. |

## Compatibility

Within `report_schema_version` 1:

- existing fields keep their names, types and meaning;
- new fields may be added, so consumers must ignore fields they do not know;
- new `kind` values may be added, so consumers must handle a kind they do not
  recognise, for example by treating it by its `status` alone;
- `status`, `outcome` and `reported_outcome` values will not change.

Removing or renaming a field, or changing a status value, requires a new
`report_schema_version`.

JSON strings escape `<`, `>` and `&` as `<`, `>` and `&`. This is
valid JSON and decodes to the original text.
