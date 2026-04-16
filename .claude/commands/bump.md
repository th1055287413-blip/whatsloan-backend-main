---
name: bump
description: Auto bump version based on conventional commits since last release.
---

**Service targets**

| Service | Variable in `version.go` | Tag prefix | Bump commit message |
|---------|--------------------------|------------|---------------------|
| api | `Version` | `api/v` | `chore: bump api version to x.y.z` |
| connector | `ConnectorVersion` | `connector/v` | `chore: bump connector version to x.y.z` |

**Path rules** (used to classify which service a commit affects)

| Service | Paths |
|---------|-------|
| connector | `cmd/connector/`, `internal/connector/`, `Dockerfile.connector` |
| api | everything else under `backend/` |

A single commit can affect both services if it touches paths from both groups.

**Steps**

1. Run `git status` to check for uncommitted changes (staged or unstaged). If there are any:
   - Stage all changed files and commit them with an appropriate conventional commit message based on the nature of the changes.
   - If changes span multiple concerns, create one commit with the most representative prefix.
2. Run `git pull --rebase` to ensure the local branch is up-to-date with the remote. If there are conflicts, stop and ask the user how to resolve them.
3. Read `backend/internal/config/version.go` and parse both `Version` and `ConnectorVersion`.
3. For **each** service, find commits since its last bump commit (see table above for message pattern). For each commit, run `git diff-tree --no-commit-id --name-only -r <hash>` to get changed files and classify the commit to api, connector, or both using the path rules. Also classify the commit type by its conventional commit prefix:
   - `feat:` → **minor**
   - anything else (`fix:`, `chore:`, etc.) → **patch**
   - Skip merge commits (prefix `merge:`)
   - If a service has no relevant commits since its last bump, it does not need a bump.
5. Determine which services need bumping. If neither needs a bump, inform the user and stop.
6. For each service that needs bumping, auto-determine the bump type:
   - If any relevant commit starts with `feat:` → **minor** (reset patch to 0)
   - Otherwise → **patch**
7. Show the user a single summary: for each service that needs bumping, show current version, relevant commits, and calculated new version.
8. Ask the user to confirm. Offer a **major** bump option for each service.
9. Edit the corresponding variable(s) in `backend/internal/config/version.go`.
10. Create one commit per service, each with its service-specific bump message (see table above).
