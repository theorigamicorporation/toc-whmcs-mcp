## What changed

<!-- One or two sentences. The PR title must be a conventional commit; it
     becomes the changelog entry. -->

## Why

<!-- The problem, not the diff. -->

## Security review

<!-- Tick what applies. Anything ticked here needs a deliberate review, not a
     rubber stamp. -->

- [ ] Touches `internal/registry/classification.go` (what an action is allowed to do)
- [ ] Touches `internal/policy`, `internal/confirm`, `internal/redact`, or `internal/untrusted`
- [ ] Touches `internal/tools/dispatch.go` (the single enforced call path)
- [ ] Adds or changes a tool, or changes what a profile permits
- [ ] Changes what data leaves the server
- [ ] None of the above

If any of the first five are ticked, answer:

- **What can an agent do after this that it could not do before?**
- **What can it see after this that it could not see before?**

## Checklist

- [ ] `just ci` passes
- [ ] `just test-safety` passes, if the safety layer was touched
- [ ] Behaviour change is reflected in `openspec/specs/` (`just spec-validate`)
- [ ] New behaviour has tests; no test needs a live WHMCS instance or credentials
- [ ] `just gen` re-run and the diff reviewed, if the registry changed
- [ ] No customer data, credentials, or real WHMCS responses in the diff or this description
