#!/usr/bin/env bash
set -euo pipefail

# sync-dev.sh -- resyncs dev onto main after a promotion PR is rebase-merged.
#
# Usage: ./scripts/sync-dev.sh
#
# Fast-forwards local main to origin/main, rebases dev onto it, and
# force-pushes dev with a lease pinned to the origin/dev fetched at the start.
# Refuses to run when origin/dev carries a commit that would not survive that
# push -- one neither in local dev nor already landed on origin/main.

info()    { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
success() { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
err()     { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------

[[ -f "go.mod" ]] || err "Must be run from the repository root"

[[ -z "$(git status --porcelain)" ]] \
  || err "Working tree is not clean -- commit or stash changes first"

git show-ref --verify --quiet refs/heads/main || err "No local main branch"
git show-ref --verify --quiet refs/heads/dev  || err "No local dev branch"

# ---------------------------------------------------------------------------
# Fast-forward local main
# ---------------------------------------------------------------------------

info "Fetching origin"
git fetch origin main dev --quiet

# Local main must never carry commits origin/main lacks; --ff-only alone would
# let that pass as a silent no-op and the rebase would target the wrong base.
AHEAD="$(git rev-list --count origin/main..main)"
[[ "$AHEAD" -eq 0 ]] \
  || err "Local main is $AHEAD commit(s) ahead of origin/main -- resolve that first"

# The lease below is pinned to the origin/dev fetched above, so it only guards
# the seconds between that fetch and the push -- it can't notice a commit that
# was already on origin/dev before this run (pushed from another machine, or
# made in the GitHub web UI) but never reached local dev. The force-push would
# silently drop such a commit, so refuse here instead. An origin/dev commit is
# safe to overwrite if local dev already has it or a patch-equivalent of it
# (git cherry dev origin/dev marks it "-"), or if origin/main does (git cherry
# origin/main origin/dev marks it "-", or omits it as reachable) -- the
# promotion's rebase-merge rewrites dev's SHAs onto main, and dev is about to
# be rebased onto main. Only a commit unaccounted for on both sides is lost.
UNSYNCED="$(comm -12 \
  <(git cherry dev origin/dev | sed -n 's/^+ //p' | sort) \
  <(git cherry origin/main origin/dev | sed -n 's/^+ //p' | sort))"
if [[ -n "$UNSYNCED" ]]; then
  printf '\033[1;31mError:\033[0m origin/dev has commit(s) missing from both local dev and origin/main:\n' >&2
  while IFS= read -r sha; do
    printf '  %s\n' "$(git log -1 --format='%h %s' "$sha")" >&2
  done <<< "$UNSYNCED"
  printf 'Bring them into local dev first (git switch dev && git pull --rebase), then re-run.\n' >&2
  exit 1
fi

git switch main --quiet
BEFORE="$(git rev-parse HEAD)"
git merge --ff-only origin/main --quiet \
  || err "Could not fast-forward main to origin/main"
AFTER="$(git rev-parse HEAD)"

if [[ "$BEFORE" == "$AFTER" ]]; then
  info "main was already at origin/main (${AFTER:0:7})"
else
  success "main fast-forwarded ${BEFORE:0:7} -> ${AFTER:0:7}"
fi

# ---------------------------------------------------------------------------
# Rebase dev
# ---------------------------------------------------------------------------

git switch dev --quiet

if git merge-base --is-ancestor main dev; then
  success "dev already contains main -- nothing to rebase"
else
  info "Rebasing dev onto main"
  if ! git rebase main; then
    git rebase --abort
    err "Rebase hit conflicts and was aborted; dev is unchanged. Resolve manually."
  fi
  success "dev rebased onto main"
fi

# ---------------------------------------------------------------------------
# Push dev
# ---------------------------------------------------------------------------

LOCAL_DEV="$(git rev-parse dev)"
REMOTE_DEV="$(git rev-parse origin/dev)"

if [[ "$LOCAL_DEV" == "$REMOTE_DEV" ]]; then
  success "dev already matches origin/dev -- nothing to push"
else
  info "Force-pushing dev (lease pinned to origin/dev at ${REMOTE_DEV:0:7})"
  git push --force-with-lease="dev:${REMOTE_DEV}" origin dev
  success "dev pushed (${REMOTE_DEV:0:7} -> ${LOCAL_DEV:0:7})"
fi
