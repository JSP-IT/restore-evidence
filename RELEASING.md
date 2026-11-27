# Releasing

Releases are cut from `main`. Release binaries are built by the
**Release binaries** workflow, which only uploads assets to an existing draft
release and never publishes it.

## Labels and release notes

Label every pull request before merging. GitHub groups the generated release
notes by these labels (see `.github/release.yml`):

| Label | Section |
| --- | --- |
| `breaking-change` | Breaking changes |
| `security` | Security |
| `enhancement` | Features |
| `bug` | Bug fixes |
| `documentation` | Documentation |
| `ci` | CI and release tooling |
| `dependencies` | Dependencies |
| `skip-changelog` | Left out of the notes |

Pull requests that only prepare a release also get the `release` label.

## Steps

1. Write `RELEASE_NOTES_vX.Y.Z.md`, then merge the release pull request into
   `main` after CI passes.
2. Tag the merge commit and push the tag. A draft release does not create its
   tag, so the tag must exist before the workflow runs:

   ```sh
   git switch main
   git pull --ff-only
   git tag -a vX.Y.Z -m "Restore Evidence vX.Y.Z"
   git push origin vX.Y.Z
   ```

3. Create a draft release for the existing tag. `--generate-notes` appends the
   labelled pull request list to the hand-written notes:

   ```sh
   gh release create vX.Y.Z --draft --verify-tag \
     --title "Restore Evidence vX.Y.Z" \
     --notes-file RELEASE_NOTES_vX.Y.Z.md --generate-notes
   ```

4. Run the workflow and wait for it to finish:

   ```sh
   gh workflow run release.yml -f tag=vX.Y.Z
   ```

   It checks that the tag is on `main` and has a draft release, runs the tests
   at the tag, builds archives for Linux, macOS, and Windows on amd64 and
   arm64, and uploads them with `checksums.txt` to the draft.

5. Review the draft assets and notes. Download at least one archive and check
   it against `checksums.txt`, and check that it contains `LICENSE`,
   `LICENSE-go`, `NOTICE` and `README.md`:

   ```sh
   gh release download vX.Y.Z --pattern 'checksums.txt' --pattern '*_linux_amd64.tar.gz'
   sha256sum --ignore-missing -c checksums.txt
   tar -tzf restore-evidence_vX.Y.Z_linux_amd64.tar.gz
   ```

   Then publish:

   ```sh
   gh release edit vX.Y.Z --draft=false --latest
   ```

6. Check that `go install github.com/JSP-IT/restore-evidence/cmd/restore-evidence@vX.Y.Z`
   works and that `restore-evidence version` prints the tag.
