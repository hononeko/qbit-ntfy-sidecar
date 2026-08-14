# Agent & Contributor Guidelines (`AGENTS.md`)

This document outlines the core architecture principles, development workflows, quality gates, and best practices for developing and maintaining `qbit-ntfy-sidecar`.

All automated agents and human contributors must adhere to the rules in this document.

---

## 1. Non-Negotiable Pre-Commit Quality Gates

Before committing or submitting changes, all of the following steps **MUST** pass cleanly:

1. **Code Formatting (`gofmt`):**
   * Code must be formatted using the official Go formatter with simplification enabled:
     ```bash
     gofmt -s -w .
     ```
   * Ensure `git diff` shows zero formatting inconsistencies.

2. **Test Suite & Race Detector:**
   * All package tests must pass with zero failures and race detection enabled:
     ```bash
     go test -v -race -coverprofile=coverage.out ./...
     ```
   * The `-race` flag is mandatory to catch concurrency issues early.

3. **Static Analysis & Linting:**
   * Run `golangci-lint` to satisfy repository rules:
     ```bash
     golangci-lint run
     ```
   * Do not ignore lint warnings using `//nolint` unless accompanied by a justifiable reason in an inline comment.

4. **Dependency Hygiene:**
   * Ensure modules and sums are clean and tidy:
     ```bash
     go mod tidy
     git diff --exit-code go.mod go.sum
     ```

---

## 2. Golang Engineering Standards

### 2.1 Context & Lifecycle Management
* **Propagate Contexts:** Every function performing I/O, network requests, long-running loops, or background tasks must accept and respect a `context.Context` parameter.
* **HTTP Requests:** Always use `http.NewRequestWithContext(ctx, ...)` instead of `http.NewRequest(...)`.
* **Graceful Termination:** Listen for `os.Interrupt` and `syscall.SIGTERM` using `signal.Notify`. Ensure background workers, periodic auto-discovery loops, coordinators, and the HTTP server shut down cleanly using `app.Cancel()` and `app.Wg.Wait()`.

### 2.2 Concurrency & State Safety
* **Mutex & State Protection:** Any shared state (e.g. `ActiveMonitors`, `Completed`, tracking counters) accessed across multiple goroutines must be synchronized using `app.Mutex` (`sync.Mutex`).
* **Non-Blocking Signaling:** Channel triggers (e.g., `WakeCh` for the grouped coordinator) must use non-blocking `select` with a `default` case to prevent deadlocks when channels are unbuffered or saturated.
* **Goroutine Leaks:** Background routines (`startupScan`, `runAutoDiscovery`, `trackTorrent`, `groupedCoordinator`) must have a deterministic exit condition tied to context cancellation (`ctx.Done()`) and decrement `app.Wg.Done()`.

### 2.3 Security & Input Sanitization
* **Network Access Control:** The `/track` endpoint must enforce IP subnet filtering using `netip.Prefix` from `ALLOWED_SUBNETS`. Requests from unauthorized remote addresses must return `403 Forbidden`.
* **Hash Validation:** All torrent hashes received via query parameters or payloads must be validated using `IsValidHash` (valid hexadecimal string, standard length) before processing.
* **Header & Log Injection Prevention:** 
  * Sanitize dynamic user inputs used in HTTP headers (e.g. `X-Title`, `X-Click`) using `SanitizeHeader` to strip carriage returns and newlines.
  * Use `%q` or structured parameters in log statements to prevent log injection from untrusted remote addresses or torrent names.
* **Secret Hygiene:** Never log sensitive credentials (`QBIT_PASS`, `NTFY_PASS`, auth cookies) in plaintext.

### 2.4 Error Handling & Logging
* **Handle Transient Failures:** Handle qBittorrent connection drops, API errors, and Ntfy rate limits (`429 Too Many Requests`) with backoff or retry logic rather than crashing.
* **Health Alerts:** When `NOTIFY_HEALTH_ERRORS` is enabled, alert on sustained connection failure thresholds and notify on recovery.
* **Clean Logging:** Keep log output concise and structured. Container orchestrators manage timestamps, so avoid redundant timestamp formatting in default log outputs.

### 2.5 Testing Best Practices
* **Deterministic Tests:** Do not rely on arbitrary `time.Sleep(...)` calls to wait for asynchronous operations in tests. Use mock servers, channels, `sync.WaitGroup`, or polling loops with timeout assertions.
* **Mock External Services:** Use `httptest.Server` to mock external endpoints (qBittorrent WebUI API and Ntfy server) to test both happy paths and edge cases (`401 Unauthorized`, `403 Forbidden`, `429 Rate Limit`, `500 Internal Server Error`, malformed JSON, connection drops).
* **Table-Driven Tests:** Structure unit tests with table-driven test cases (`[]struct{ name string, ... }`) for parsing, formatting, network filtering, and edge cases.

---

## 3. Container & Docker Best Practices

Because this service is delivered as a containerized sidecar running in Kubernetes and Docker Compose environments alongside qBittorrent:

### 3.1 Minimal & Rootless Containers
* **Static Binary:** Build static Go binaries with CGO disabled:
  ```dockerfile
  CGO_ENABLED=0 go build -ldflags="-w -s" -o sidecar .
  ```
* **Distroless Base:** Use `gcr.io/distroless/static-debian12` which includes CA root certificates required for HTTPS connections to `ntfy.sh` or secure upstream endpoints while keeping the image footprint minimal.
* **Low Resource Footprint:** Maintain minimal CPU (10m) and memory (32Mi) overhead so it runs lightweight alongside torrent clients.

### 3.2 HTTP Server Hardening
* **Configured Timeouts:** All `http.Server` instances must explicitly define defensive timeouts (`ReadTimeout`, `WriteTimeout`, `IdleTimeout`).

---

## 4. Repository Structure & Conventions

```
.
├── .github/
│   └── workflows/          # GitHub Actions CI/CD workflows (validate, cd, pr)
├── docs/                   # Documentation assets & screenshots
├── config.go               # Environment variable parsing & configuration types
├── config_test.go          # Configuration tests
├── monitor.go              # Torrent monitoring, auto-discovery & grouped coordination logic
├── monitor_test.go         # Monitor & coordinator tests
├── ntfy.go                 # Ntfy.sh HTTP client & message formatting
├── ntfy_test.go            # Ntfy payload & delivery tests
├── qbit.go                 # qBittorrent WebUI client & authentication
├── qbit_test.go            # qBittorrent client tests
├── server.go               # HTTP server handlers & IP filter middleware
├── server_test.go          # HTTP endpoint & middleware tests
├── utils.go                # Formatting utilities, progress bars & hash validation
├── utils_test.go           # Utility tests
├── main.go                 # Application entrypoint & graceful shutdown orchestration
├── main_test.go            # Application lifecycle tests
├── Dockerfile              # Multi-stage distroless container build
└── AGENTS.md               # Repository rules & contributor guidelines
```

### Commit Guidelines
Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:
* `feat:` A new feature or capability
* `fix:` A bug fix or stability correction
* `refactor:` Code restructuring without behavioral changes
* `test:` Adding or improving tests
* `docs:` Documentation updates
* `chore:` Maintenance, dependency bumps, tooling updates
