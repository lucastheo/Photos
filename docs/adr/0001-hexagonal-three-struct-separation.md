# ADR 0001: Hexagonal Architecture, Strict 3-Struct Separation & Consumer-Side Interfaces

- **Status:** Accepted
- **Date:** 2026-11-17

## Context

In Go web applications, sharing a single struct across HTTP handlers (`json` tags), business logic, and SQL repositories (`db` tags) tightly couples transport contracts to database schemas. Additionally, placing large repository interfaces in a central `domain` package violates the Go proverb *"Accept interfaces, return structs"*.

## Decision

1. **Strict 3-Struct Separation:**
   - `internal/adapter/httpapi` defines HTTP Request/Response DTO structs (`json:"..."` tags only) and explicit DTO ↔ Domain mappers.
   - `internal/domain` defines pure Domain Objects with **zero** struct tags (`no json`, `no db`) and **zero** interfaces, importing only the Go standard library (`$gostd`).
   - `internal/adapter/sqlite` defines Database Entity structs (`db:"..."` tags only) and explicit Entity ↔ Domain mappers.
2. **"Accept Interfaces, Return Structs":**
   - Every constructor (`New...`) and function returns a concrete struct, enforced by `ireturn` in `golangci-lint`.
   - Narrow interfaces are defined exclusively in the consuming packages (`internal/usecase` and `internal/adapter/httpapi`).
3. **Automated Boundary Enforcement:**
   - `depguard` in `.golangci.yml` prevents `httpapi`, `sqlite`, `media`, and `usecase` from importing one another directly.

## Consequences

- Each layer can be unit-tested in complete isolation with lightweight in-memory fakes (> 90% coverage without reflection mocking libraries).
- Schema changes in SQLite never accidentally break JSON API payloads, and vice versa.
