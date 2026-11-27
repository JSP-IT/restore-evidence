# Evidence limitations

A `restore-evidence` report is a record of specific checks made at a specific
time. Read it for exactly that and nothing more. Every report carries a short
version of this list.

## What a `pass` shows

A `pass` means that when the tool ran:

- every file listed in the manifest existed in the supplied directory as a
  regular file at exactly the listed path;
- every listed size and SHA-256 digest matched;
- the directory-wide count, total size and unlisted-file checks requested by
  the manifest matched;
- every supplied result was *reported by the operator* as `pass`.

## What a report does not show

- **That the backup can be restored again.** A report describes one restore.
  It says nothing about other backups, other restore targets, other operators
  or later attempts.
- **That the restored data is complete or correct.** Only the listed checks
  were made. Files the manifest does not mention, file contents beyond the
  digest, permissions, ownership, timestamps, extended attributes and database
  consistency are not examined.
- **That the system works.** Whether an application starts or a database
  accepts queries is only in the report if the operator supplied it as a
  result, and then it is reported, not verified.
- **Where the data came from.** A matching digest shows the restored file
  equals the digest in the manifest. It is only as trustworthy as whoever
  produced the manifest. If the manifest was generated from the restored data,
  the check shows nothing.
- **Compliance.** The report is not a certification against any standard or
  regulation. It may be useful input to an audit. Whether it is sufficient is
  for the auditor to decide.
- **Integrity of the report itself.** Reports are not signed and are not
  tamper-evident. Anyone who can write the file can change it, and anyone with
  the manifest and directory can regenerate it. If you need tamper evidence,
  store the report in a write-once system or sign it with your own tooling.
- **Recovery time.** The timestamps in the report are copied from the manifest.
  The tool does not measure them.

## Things that can make a report misleading

- The restored directory changed while the tool was running.
- The manifest was edited after the restore to match what was restored. The
  `manifest.sha256` field lets you compare a report against a manifest you have
  kept separately.
- `allow_unlisted: true` was set, so extra files were not reported as failures.
- `-no-hash` was used, or digests were left out of the manifest. These checks
  are reported as `not_performed` and make the outcome `incomplete`, never
  `pass`.
- On case-insensitive file systems, names that differ only in case are treated
  as different. See [Paths](manifest.md#paths).

## Reading statuses honestly

- `verified` is the only status that means the tool saw a match.
- `not_performed` is not a soft pass. It means nothing is known about that check.
- A supplied result is the operator's statement, shown with its reported
  outcome and the reference they gave.
