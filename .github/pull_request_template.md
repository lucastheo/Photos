## Summary

<!-- Briefly describe what this PR implements and why -->

## Mandatory Quality Gate Checklist

- [ ] **Follow the Project Design:** Follows the project architecture and design rules defined in `AGENTS.md` and `docs/SPECIFICATION.md`.
- [ ] **File & Function Size Limits:** `make lint-max-file` passes (all source files ≤ 1,000 lines, all functions ≤ 60 lines).
- [ ] **Linters & GoDoc:** `make lint` passes with zero errors (`golangci-lint`, `revive` GoDoc checks, `eslint`, `tsc --noEmit`).
- [ ] **Unit & Integration Tests (> 90% Coverage):** `make test-unit`, `make test-integration`, and `make test-coverage` pass with strictly **> 90.0%** coverage.
- [ ] **Playwright Black-Box E2E (if UI/API changed):** `make test-e2e` passes all end-to-end browser journeys.
- [ ] **Documentation Updated:** Updated GoDoc comments, `docs/openapi.yaml`, `docs/FEATURES.md`, or `docs/adr/` if applicable.
