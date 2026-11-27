# Security Policy

Please report suspected vulnerabilities privately through GitHub's private
vulnerability reporting: open the repository's **Security** tab and choose
**Report a vulnerability**. Please do not open public issues for security
problems.

## Supported versions

Only the latest release receives fixes.

## Scope

`restore-evidence` reads a manifest and a restored directory supplied by the
user, and writes report files. Issues of particular interest:

- reading, hashing or counting anything outside the restored directory, for
  example through symbolic links, junctions, `..` segments or absolute paths;
- writing to, or changing, anything inside the restored directory;
- replacing an existing file without `-force`, or replacing a directory,
  symbolic link or the manifest with `-force`;
- markup or script injection into the HTML report;
- file contents, or host paths of the manifest or restored directory,
  appearing in a report or error message;
- a report showing `verified` or `pass` for something that was not checked;
- any way to make the tool execute a command or open a network connection.

The tool does not defend against an attacker who can change the restored
directory or the manifest while it runs, or who can modify the binary or the
reports afterwards. See [Evidence limitations](docs/evidence-limitations.md).
