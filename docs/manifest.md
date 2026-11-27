# Manifest format

A manifest is a UTF-8 JSON file that describes one restore test: what was
restored, what the restored directory should contain, and any results the
operator observed. You write it, or generate it, before or after the restore.
`restore-evidence` only reads it.

This page describes `schema_version` 1, the only version so far.

## Example

```json
{
  "schema_version": 1,
  "test": {
    "id": "RT-2026-11-001",
    "title": "Quarterly restore test of the inventory database",
    "system": "inventory",
    "environment": "isolated test host",
    "backup_reference": "nightly snapshot 2026-11-19",
    "backup_taken_at": "2026-11-19T23:00:00Z",
    "restore_started_at": "2026-11-20T09:14:00Z",
    "restore_completed_at": "2026-11-20T09:15:40Z",
    "performed_by": "operations on-call",
    "notes": "Restored to /srv/restore-test on the test host."
  },
  "expected": {
    "file_count": 2,
    "total_bytes": 188,
    "allow_unlisted": false
  },
  "files": [
    { "path": "db/customers.csv", "size": 111, "sha256": "2bca402a…" },
    { "path": "db/orders.csv", "size": 77, "sha256": "84f9effd…", "sensitive": true }
  ],
  "supplied_results": [
    {
      "id": "service-start",
      "description": "The restored service started and answered its health endpoint.",
      "outcome": "pass",
      "evidence_ref": "CHG-1234",
      "command": "curl -fsS http://127.0.0.1:8080/healthz",
      "notes": "Took 40 seconds to become healthy."
    }
  ]
}
```

(Digests shortened; a real manifest needs all 64 hex characters.)

## Fields

### Top level

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `schema_version` | integer | yes | Must be `1`. |
| `test` | object | yes | Describes the restore test. |
| `expected` | object | no | Directory-wide expectations. |
| `files` | array | no* | Expected files. |
| `supplied_results` | array | no* | Results observed by the operator. |

\* At least one of `files` and `supplied_results` must be non-empty.

Unknown fields are rejected at every level. This catches typos such as
`"sha265"` that would otherwise silently turn a check off.

### `test`

All fields are free text written by the operator. The report repeats them as
given.

| Field | Required | Notes |
| --- | --- | --- |
| `id` | yes | Your identifier for the test. Up to 512 characters. |
| `title` | yes | Up to 512 characters. |
| `system`, `environment`, `backup_reference`, `performed_by` | no | Up to 512 characters each. Prefer a role or team over a person's name in `performed_by`. |
| `backup_taken_at`, `restore_started_at`, `restore_completed_at` | no | RFC 3339 timestamps, for example `2026-11-20T09:14:00Z`. The order backup, start, completion is checked when the values are present. |
| `notes` | no | Up to 20,000 characters. |

### `expected`

| Field | Type | Notes |
| --- | --- | --- |
| `file_count` | integer ≥ 0 | Expected number of regular files under the restored directory, counted recursively. |
| `total_bytes` | integer ≥ 0 | Expected total size of those files. |
| `allow_unlisted` | boolean | Default `false`: every regular file in the restored directory must be listed in `files`. Set `true` when `files` is a sample. |

The manifest must be consistent with itself. With `allow_unlisted: false`,
`file_count` must equal the number of entries in `files`, and `total_bytes`
must equal the sum of their sizes when every entry has a size. With
`allow_unlisted: true`, neither may be smaller than what the listed entries add
up to.

### `files[]`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `path` | string | yes | Relative to the restored directory. See [Paths](#paths). |
| `size` | integer ≥ 0 | no | Expected size in bytes. Without it, the `size` check is `not_performed`. |
| `sha256` | string | no | 64 hexadecimal characters; case does not matter. Without it, the `sha256` check is `not_performed`. |
| `sensitive` | boolean | no | Replace this path with a label in every output. |

Entries are reported sorted by path, whatever order they appear in.

### `supplied_results[]`

A supplied result is something the operator observed and reports. The tool does
not check it. It is copied into the report and counted separately from the
tool's own checks.

| Field | Required | Notes |
| --- | --- | --- |
| `id` | yes | Unique within the manifest. |
| `description` | yes | What was checked. |
| `outcome` | yes | `pass`, `fail` or `not_performed`. |
| `evidence_ref` | no | A ticket, log or document reference, as text. It is not opened or followed. |
| `command` | no | The command you ran, as documentation. **It is never executed.** |
| `notes` | no | Free text. |

A supplied `fail` makes the report outcome `fail`. A supplied `not_performed`
makes it at least `incomplete`.

## Paths

A path must:

- be relative to the restored directory and use `/` as the separator, on every
  platform;
- be clean: no `.` or `..` segments, no empty segments (`a//b`), no leading or
  trailing `/`;
- contain no backslash, no control characters, and no drive letter prefix such
  as `C:`;
- be valid UTF-8 and at most 4,096 bytes;
- be unique, and not be the parent directory of another listed path.

Paths are compared exactly, byte for byte, with the names the directory walk
returns. On a case-insensitive file system, a manifest entry `Data.csv` does not
match a restored file `data.csv`: the entry fails `presence` and the file
counts as unlisted. Normalise the case of names in the manifest to match the
restore.

## Limits

| Limit | Value |
| --- | --- |
| Manifest size | 64 MiB |
| `files` entries | 500,000 |
| `supplied_results` entries | 10,000 |

## Creating a manifest

Generate the manifest from the source data before the backup, or from a
trusted copy, and never from the restored data: a manifest made from the
restored files would simply describe whatever was restored. Keep it somewhere
the restore process cannot write to.

On Linux or macOS, `sha256sum` (or `shasum -a 256`) and `stat` give you the
digests and sizes. `restore-evidence` does not generate manifests. Generating
them is deliberately left to your own tooling, so that the expected values and
the check stay independent.

## Schema compatibility

- `schema_version` is an integer. This build accepts exactly `1` and rejects
  any other value with a message naming the versions it supports.
- Version 1 is frozen. Because unknown fields are rejected, any new field,
  including an optional one, requires a new `schema_version`.
- Later releases are intended to keep accepting `schema_version` 1 and to
  produce the same checks for the same manifest and directory. Dropping support
  for a schema version is a breaking change and will be called out in the
  release notes. Wording of `reason` text may change in any release.
- A manifest written for a newer schema version is rejected by an older build
  instead of being half understood.

The report has its own version, described in [Report format](report.md).
