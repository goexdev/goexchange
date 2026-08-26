# CI/CD Workflows

GitHub Actions workflows for continuous integration.

## Workflows

### `ci.yml` - Main CI Pipeline

Runs on every push and PR to `main`. Includes 5 jobs:

| Job | Purpose | Trigger |
|---|---|---|
| `backend` | Go build + test + golangci-lint | All pushes/PRs |
| `frontend` | npm ci + ESLint + build + JSON validation | All pushes/PRs |
| `i18n-check` | Verify all locales have all English keys | After frontend |
| `ui-tests` | Playwright UI integration tests | All pushes/PRs |
| `ci-status` | Aggregate check (fails CI if any required job fails) | After all above |

## Services

CI uses these Docker services:
- **PostgreSQL 16** (port 5432) - for backend tests
- **Redis 7** (port 6379) - for rate limiting / cache tests

## Critical i18n Protection

The **frontend** job runs `npm run lint` which uses the custom ESLint rule
`no-hardcoded/no-hardcoded-jsx-text` (defined in
`web/eslint-plugins/no-hardcoded-jsx-text.js`).

**This means any PR that introduces hardcoded English in JSX will FAIL CI.**

This is the permanent protection that prevents i18n regression.

## PR Template

See `.github/pull_request_template.md` - includes i18n-specific checklist.

## Local Testing

Before pushing, run the same checks locally:

```bash
# Backend
make build
make test
make lint

# Frontend
cd web
npm ci
npm run lint
npm run build

# Full i18n check
node -e "$(cat .github/workflows/i18n-check-script.js)"  # if extracted
```

## Required Secrets

No secrets required for current CI. If you add integration tests with
external services, add them via GitHub repo Settings > Secrets.

## Adding New Jobs

When adding a new check:
1. Add it as a new job in `ci.yml`
2. Add the job name to `ci-status.needs` array
3. Update PR template checklist
4. Test locally first
5. Document in this README

## Troubleshooting Failed CI

| Failure | Likely Cause | Fix |
|---|---|---|
| `backend` test failure | Test logic issue | Run `make test` locally |
| `frontend` lint failure | Hardcoded English | Use `t()` from react-i18next |
| `frontend` build failure | TypeScript error | Run `npm run build` locally |
| `i18n-check` failure | Missing translation key | Add key to en.json (warning only) |
| `ui-tests` failure | Test logic / Playwright issue | Check screenshots in test output |
