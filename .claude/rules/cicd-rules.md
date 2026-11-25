# CI/CD Rules (PART 27)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO

- Never use Makefile in CI, floating third-party action references, or skip required security checks.

## CRITICAL - ALWAYS DO

- Maintain equivalent provider-native workflows for GitHub, GitLab, Gitea, Forgejo, and Jenkins; pin third-party actions by full SHA, build all required platforms, run every security gate, and check provider CI after pushes.

## Key rules summary

- GitHub, GitLab, Gitea, Forgejo, and Jenkins workflow files are required. GitHub Actions syntax is shared by the Gitea and Forgejo workflows.
- Check cross-references in AI.md before implementing a dependent feature.
- Update README.md and docs/ whenever shipped behavior changes.

## KEY DECISIONS

| Question | Answer | Spec reference |
|---|---|---|
| When are release workflows added? | After implementation, tests, and lint are clean. | PART 27 |
| What proves a pushed change is complete? | The actual provider CI run is green; local checks alone are insufficient. | PART 27 |

## TERMINOLOGY

| Term | Meaning |
|---|---|
| Git provider | GitHub, GitLab, Gitea, Forgejo, or Jenkins deployment |
| Stable release | A tagged release built and published by the provider workflow |

## QUICK REFERENCE

- Keep equivalent build, test, lint, coverage, secret, dependency, and image security gates across providers.
- Keep PR workflows non-publishing and prevent secrets from reaching fork PRs.
- Check CI after every push and fix failures before reporting completion.

For complete details, see AI.md PART 27.
