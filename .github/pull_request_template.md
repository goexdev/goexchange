## Description

<!-- Brief description of changes -->

## Type of Change

- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] Documentation update

## Checklist

### Backend (Go)
- [ ] `go build ./...` passes
- [ ] `go test ./... -timeout 5m` passes
- [ ] `golangci-lint run` passes (no new warnings)
- [ ] No new hardcoded strings that should be in constants/config
- [ ] Audit log entries added for sensitive operations
- [ ] Database migrations are reversible

### Frontend (React)
- [ ] `npm run lint` passes (i18n check)
- [ ] `npm run build` passes (TypeScript compiles)
- [ ] **All visible text uses `t()` from react-i18next** (NO hardcoded English)
- [ ] New translation keys added to `en.json`
- [ ] If new keys, ideally translated to other 7 languages
- [ ] No `console.log` or `debugger` statements
- [ ] Mobile responsive (if UI change)

### Security
- [ ] No secrets in code
- [ ] No SQL injection risk (parameterized queries)
- [ ] Authorization checks for admin endpoints
- [ ] Rate limiting considered for new endpoints
- [ ] Input validation (max lengths, enums)

### i18n Specific
- [ ] All new user-facing text uses `t('section.keyName')`
- [ ] No literal English text in JSX (will be caught by ESLint)
- [ ] New en.json keys are descriptive and follow naming convention
- [ ] For Russian/Arabic: use appropriate script
- [ ] For Persian: `dir="rtl"` considered if needed

## Testing

<!-- How was this tested? -->

## Screenshots (if applicable)

<!-- Add screenshots -->

## Related Issues

<!-- Link related issues: Fixes #123, Closes #456 -->

## Deployment Notes

<!-- Any special deployment steps needed? -->
