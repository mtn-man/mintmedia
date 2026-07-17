# Promoting dev to main

Day-to-day work happens on `dev`; `main` is kept release-ready at all times.
This is the checklist for moving a batch of `dev` work onto `main`. It is not
the same as cutting a release -- see [Releasing](#releasing-is-separate)
below.

## Steps

1. **Confirm `dev` is in a promotable state.** Working tree clean, all
   intended commits present, nothing half-finished left on the branch.
2. **Run `/code-review`** against the diff `dev` carries over `main`. Address
   any findings with new commits on `dev` before opening the PR.
3. **Open the promotion PR:**
   ```
   gh pr create --base main --head dev
   ```
   Keep the title short; use the body for a bullet summary of what's being
   promoted.
4. **Wait for green CI** on the PR (build + vet + `go test -race ./...` on
   ubuntu-latest and macos-latest).
5. **Merge with a rebase merge, not a squash merge** -- this preserves the
   atomic, per-concern commits already made on `dev` as a linear history on
   `main`, rather than flattening a batch of unrelated changes into one
   commit:
   ```
   gh pr merge --rebase
   ```
6. **Immediately rebase `dev` onto the new `main`.** Do this right after the
   merge, not before the next round of `dev` work starts -- otherwise the
   next promotion PR will show false conflicts against commits that already
   landed:
   ```
   git switch main && git pull
   git switch dev && git rebase main
   git push --force-with-lease
   ```

## Releasing is separate

Promoting `dev` to `main` does not mean a release should follow immediately.
Don't cut a release just because `main` has a clean diff over the last
release -- wait until there's a real user-facing change (a feature or a
visible fix), not just internal refactors or chores, unless there's an
explicit forcing reason. See `./scripts/release.sh` for the release process
itself, which is run from `main` with a clean tree and green CI on the tip
commit.
