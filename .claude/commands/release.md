---
name: release
description: Tag current version and push to trigger CI/CD deployment.
---

**Service targets**

| Service | Variable in `version.go` | Tag format | Workflow triggered |
|---------|--------------------------|------------|--------------------|
| api | `Version` | `api/v{version}` | `deploy-api.yml` |
| connector | `ConnectorVersion` | `connector/v{version}` | `deploy-connector.yml` |

**Steps**

1. Ask the user which service to release: **api** or **connector**.
2. Run `git status` to ensure the working tree is clean. If there are uncommitted changes, warn the user and stop.
3. Read `backend/internal/config/version.go` and parse the current version number from the corresponding variable (see table above).
4. Check if the tag already exists with `git tag -l`. If it does, warn the user and stop.
5. Show the user the current version and ask for confirmation: "Release **{tag}**?"
6. If confirmed:
   - Create tag: `git tag {tag}`
   - Push commits and tag: `git push origin main --tags`
7. Show the user the result and remind them the corresponding GitHub Actions deployment has been triggered.
