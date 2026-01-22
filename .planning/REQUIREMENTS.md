# Requirements: GCC Receiver-Side BWE v1.2

**Defined:** 2026-01-22
**Core Value:** Automated E2E testing to validate BWE behavior under realistic conditions

## v1.2 Requirements

Requirements for E2E testing milestone.

### Browser Automation

- [ ] **BROWSER-01**: Automated Chrome REMB verification replaces manual interop test
- [ ] **BROWSER-02**: Tests run in headless mode without display server
- [ ] **BROWSER-03**: Programmatic WebRTC stats extraction via getStats() API

### Network Simulation

- [ ] **NET-01**: Latency injection with constant and variable delay patterns
- [ ] **NET-02**: Bandwidth throttling to test rate adaptation behavior
- [ ] **NET-03**: Packet jitter simulation with variable inter-packet delay
- [ ] **NET-04**: Packet loss patterns including random and burst loss

### Integration Testing

- [ ] **INT-01**: Full Pion PeerConnection E2E tests (Pion-to-Pion flow)
- [ ] **INT-02**: Multi-stream scenarios (audio + video, multiple tracks)
- [ ] **INT-03**: Stream timeout and recovery tests
- [ ] **INT-04**: Mid-call renegotiation scenarios (track add/remove)

### CI Integration

- [ ] **CI-01**: GitHub Actions workflow for automated test runs on push/PR
- [ ] **CI-02**: Docker-based Chrome for reproducible browser environment
- [ ] **CI-03**: Parallel test execution for faster feedback
- [ ] **CI-04**: Performance regression detection tracking benchmark changes

## Future Requirements

Deferred to future milestones.

### Advanced Testing

- **ADV-01**: Multi-browser testing (Firefox, Safari)
- **ADV-02**: Reference trace extraction from browser internals
- **ADV-03**: Full RFC 8867 test scenario compliance

## Out of Scope

| Feature | Reason |
|---------|--------|
| Mobile browser testing | Adds complexity, desktop Chrome sufficient for v1.2 |
| Visual quality metrics (VMAF/PSNR) | Not relevant for bandwidth estimation testing |
| Real tc/netem in CI | Requires elevated privileges, use Toxiproxy/vnet instead |
| Exhaustive parameter sweeps | Focus on representative scenarios, not combinatorial explosion |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| BROWSER-01 | Phase 8 | Pending |
| BROWSER-02 | Phase 8 | Pending |
| BROWSER-03 | Phase 8 | Pending |
| NET-01 | Phase 7 | Pending |
| NET-02 | Phase 7 | Pending |
| NET-03 | Phase 7 | Pending |
| NET-04 | Phase 7 | Pending |
| INT-01 | Phase 9 | Pending |
| INT-02 | Phase 9 | Pending |
| INT-03 | Phase 9 | Pending |
| INT-04 | Phase 9 | Pending |
| CI-01 | Phase 10 | Pending |
| CI-02 | Phase 10 | Pending |
| CI-03 | Phase 10 | Pending |
| CI-04 | Phase 10 | Pending |

**Coverage:**
- v1.2 requirements: 15 total
- Mapped to phases: 15
- Unmapped: 0

**Phase 6 Note:** Phase 6 (Test Infrastructure Foundation) has no direct requirements but is foundational infrastructure enabling Phases 7-10. Its success criteria are derived from what subsequent phases need to function.

---
*Requirements defined: 2026-01-22*
*Last updated: 2026-01-22 - Phase mappings added after roadmap creation*
