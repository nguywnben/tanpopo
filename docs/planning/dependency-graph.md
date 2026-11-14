# Đồ thị Phụ thuộc Phi Chu trình (`DAG`), Đường Găng, Quy trình Môi trường & Thứ tự Khởi chạy — Tanpopo

> **Tài liệu Điều phối Phụ thuộc & Hạ tầng Dựng (Phase ASTRA A09 — Gate G4: `READY_FOR_REVIEW` | `DRAFT — EXECUTION BASELINE UNLOCKED`)**
> - **Mã tài liệu:** `PLAN-DAG-01` | **Phiên bản:** `0.1.0-step09-draft` | **Ngày lập:** `2026-11-14`
> - **Vai trò biên soạn:** Staff Engineer, Technical Program Manager & QA Lead
> - **Bảo chứng Toán học của Đồ thị (`Verified Acyclic`):** Mọi cạnh phụ thuộc `TP-IMP-u -> TP-IMP-v` trên toàn bộ `47` đơn vị đều thỏa mãn bất biến chỉ số tăng ngặt `u < v`, chứng minh đồ thị là **Đồ thị Có hướng Phi Chu trình (`DAG` — `0` vòng lặp, `0` định danh phụ thuộc mồ côi)**.

---

## 1. Sơ đồ Phụ thuộc Toàn cục (`End-to-End Implementation Dependency DAG`)

```mermaid
flowchart TD
    GateOwner["CHỐT CHẶN GỐC: Product Owner Duyệt Gate G1-G3 + CR-01..10 & Cấp phép SPIKE"]

    subgraph M0["M0: Đóng Lỗ hổng Spec A08 & Spikes"]
        U01["TP-IMP-0001: Spec Closure A (SM, SRS, AC)"]
        U02["TP-IMP-0002: Spec Closure B (Proto, OpenAPI, CLI)"]
        U03["TP-IMP-0003: Spec Closure C (DDL, DEK, RLS, Valkey)"]
        U04["TP-IMP-0004: Spikes SPIKE-01..04 & Freeze Wasm ABI"]
    end

    subgraph M1["M1: Nền móng & Lát cắt Dọc External Adapter"]
        U05["TP-IMP-0005: Monorepo, Apache-2.0 & Floor Guard CI"]
        U06["TP-IMP-0006: Hermetic Testkit & FakeLLMServer"]
        U07["TP-IMP-0007: Storage Port & SQLite WAL"]
        U08["TP-IMP-0008: Envelope Vault (KEK/Org/Project DEK)"]
        U09["TP-IMP-0009: IAM, RBAC, Virtual Keys & ConfigSnapshot"]
        U10["TP-IMP-0010: SSRFGuard & Blind Credential Broker"]
        U11["TP-IMP-0011: Package Verifier, SM-01 & tanpopo.lock"]
        U12["TP-IMP-0012: Tier-1 Declarative Engine"]
        U13["TP-IMP-0013: Tier-2 Wazero Wasm Sandbox Pool"]
        U14["TP-IMP-0014: Plugin SDK & Cassette Test Harness"]
        U15["TP-IMP-0015: /v1/chat SSE + EXT-ACCEPT-01 Vertical Slice"]
    end

    subgraph M2["M2: Hoàn chỉnh MVP v0.1 (Routing, FinOps, Astryx UI, CLI, Ops)"]
        U16["TP-IMP-0016: /v1/responses, embeddings, models & blob_ref"]
        U17["TP-IMP-0017: Lossless /native/{dialect}/*"]
        U18["TP-IMP-0018: 5 Official Reference Adapters v0.1"]
        U19["TP-IMP-0019: Hierarchical Policy & Rate Limiter"]
        U20["TP-IMP-0020: 6-Step Capability Router & SM-04 Breaker"]
        U21["TP-IMP-0021: Two-Phase Micro-USD Budget Ledger"]
        U22["TP-IMP-0022: OTel Exporter & Privacy Redactor"]
        U23["TP-IMP-0023: JCS Audit Hash-Chain & 4-Level Kill-Switch"]
        U24["TP-IMP-0024: Admin API Part 1 (IAM, Keys, Sessions)"]
        U25["TP-IMP-0025: Admin API Part 2 (Plugins, Routes, FinOps, Ops)"]
        U26["TP-IMP-0026: Astryx Console Setup, Figtree Local & AppShell"]
        U27["TP-IMP-0027: UI SCR-01..02 (Setup Wizard <3m & Overview)"]
        U28["TP-IMP-0028: UI SCR-03..07 (Providers, Models & Plugins)"]
        U29["TP-IMP-0029: UI SCR-08, 09, 13 (Routes, Policies & Keys)"]
        U30["TP-IMP-0030: UI SCR-11, 12, 14, 16 (Traces, FinOps, Sec, Ops)"]
        U31["TP-IMP-0031: Unified tanpopo CLI & Embedded SPA Binary"]
        U32["TP-IMP-0032: Doctor, GitOps, Backup, Shredding & Preflight"]
        U33["TP-IMP-0033: Multi-OS Installers, OCI, SBOM & Cosign"]
    end

    subgraph M3["M3: v0.2 OAuth 2.1, Marketplace & MCP Hub"]
        U34["TP-IMP-0034: Authorized OAuth 2.1 Single-Flight Broker"]
        U35["TP-IMP-0035: Marketplace Index, OCI Puller & Bedrock/Azure"]
        U36["TP-IMP-0036: MCP Hub Core & JCS schema_hash Pinning"]
        U37["TP-IMP-0037: MCP HITL Destructive Approval & Webhooks"]
        U38["TP-IMP-0038: Route Explain, Playground & MCP/Approval UI"]
    end

    subgraph M4["M4: v0.3 Cụm HA Postgres/Valkey, SSO/KMS & A2A"]
        U39["TP-IMP-0039: PostgreSQL 16 FORCE RLS & SQLite->PG Migrate"]
        U40["TP-IMP-0040: Valkey {org_id} Lua, <5s Revoke & Helm HA"]
        U41["TP-IMP-0041: OIDC SSO, Four-Eyes, Cloud KMS & Cache"]
        U42["TP-IMP-0042: Tier-3 Sidecar mTLS & A2A Plane (SM-09)"]
    end

    subgraph M5["M5: v1.0 GA Hardening, Air-Gap, Docs & Release"]
        U43["TP-IMP-0043: k6 Load, SLO Verification & Chaos/DR Drills"]
        U44["TP-IMP-0044: 4h Fuzzing, N-1 Shim, Multi-Browser & Pen-Test"]
        U45["TP-IMP-0045: Offline Air-Gap Bundle & Multi-Org Console GA"]
        U46["TP-IMP-0046: Docs Portal, OpenAPI Sync & Repo Polish"]
        U47["TP-IMP-0047: Release Dossier & HUMAN OWNER SIGN-OFF GATE"]
    end

    GateOwner --> U01 --> U02 --> U03
    U02 & U03 --> U04
    U03 --> U05
    U05 --> U06 & U07
    U07 --> U08 --> U09
    U04 & U08 --> U10
    U07 & U09 --> U11
    U04 & U10 & U11 --> U12 & U13
    U06 & U12 & U13 --> U14
    U09 & U12 & U13 & U14 --> U15
    U15 --> U16 & U17 & U19
    U14 & U16 & U17 --> U18
    U16 & U19 --> U20 --> U21 --> U22 --> U23 --> U24
    U12 & U20 & U21 & U24 --> U25
    U05 & U24 --> U26
    U25 & U26 --> U27 & U28 & U29 & U30
    U14 & U25 & U27 & U28 & U29 & U30 --> U31 --> U32 --> U33
    U10 & U24 --> U34
    U11 & U18 & U34 --> U35
    U10 & U19 & U34 --> U36 --> U37 --> U38
    U07 & U21 & U23 --> U39
    U33 & U39 --> U40
    U34 & U39 --> U41
    U13 & U21 & U39 --> U42
    U40 & U42 --> U43
    U38 & U41 & U42 --> U44
    U35 & U40 & U41 --> U45
    U43 & U44 & U45 --> U46 --> U47
```

---

## 2. Đường Găng (`Critical Path`), Phân Làn Song song (`Worktree Lanes`) & Thứ tự 5 Đơn vị Đầu Tiên

### 2.1. Chuỗi Đường Găng Dài Nhất (`Critical Path`)
Mọi sự chậm trễ trên chuỗi **22 chặng liên tiếp cho `MVP v0.1`** (và thêm **4 chặng cho `v1.0 GA`**) dưới đây sẽ kéo lùi trực tiếp ngày sẵn sàng phát hành:
- **Đường găng tới `MVP v0.1`:** `TP-IMP-0001` → `0002` → `0003` → `0005` → `0007` → `0008` → `0009` → `0011` → `0013` → `0014` → `0015` → `0016` → `0020` *(qua `0019` song song)* → `0021` → `0022` → `0023` → `0024` → `0025` → `0027..0030` *(song song 4 nhánh)* → `0031` → `0032` → `0033`.
- **Đường găng tiếp nối tới `v1.0 GA`:** `0033` + `0039` → `0040` → `0043` *(song song `0044`, `0045`)* → `0046` → `0047`.

### 2.2. Các Cửa sổ Chạy Song song Hiệu suất Cao Qua `git worktree`
Khi dùng nhiều AI Agents thực thi đồng thời (mỗi Agent một thư mục `git worktree` riêng), có **4 điểm nở rộng song song lớn** không đụng độ tệp:
1. **Ngay sau `TP-IMP-0003`:** Chạy song song `TP-IMP-0004` (`tools/spikes/*` có `tools/spikes/go.mod` cô lập riêng biệt) và `TP-IMP-0005` (`go.mod` gốc, `web/console`, `.github/workflows`, `.gitignore`).
2. **Ngay sau `TP-IMP-0011` + `0010`:** Chạy song song `TP-IMP-0012` (`internal/extension/declarative`) và `TP-IMP-0013` (`internal/extension/wasm`).
3. **Ngay sau `TP-IMP-0015`:** Chạy song song 3 làn: Làn Giao thức & Official Adapters (`0016`, `0017`, `0018`), Làn Chính sách/Định tuyến/Sổ cái (`0019` → `0023`), và (từ `0024`) Làn Khung Giao diện Astryx (`0026`).
4. **Ngay sau `TP-IMP-0025` + `0026`:** Chạy **song song 4 làn giao diện độc lập** (`0027`, `0028`, `0029`, `0030` — mỗi đơn vị sở hữu tập trang màn hình riêng trong `web/console/src/pages/*`), đồng thời ở backend có thể tách nhánh sớm cho `0034` (`OAuth 2.1`) và `0039` (`PostgreSQL Driver`)!

### 2.3. Thứ tự 5 Đơn vị Đầu Tiên Có Thể Bắt đầu Ngay Khi Mở Khóa (`Conditional First 5 Units`)

> **Trạng thái hiện thực hôm nay:** **`0` đơn vị đang `READY`** vì `Gate G1–G3` và `CR-01`..`10` đang chờ Product Owner phê duyệt (`GAP-01`).
> **Ngay khoảnh khắc Product Owner phê duyệt `Gate G1–G3` + `CR-01`..`10` (và cấp phép chạy Spikes), 5 đơn vị đầu tiên chuyển sang `READY` theo đúng thứ tự sau:**

1. **[`TP-IMP-0001`](./units/TP-IMP-0001.md)** *(Tuần tự #1)* — Đóng đặc tả Lô A: Vá điểm chết máy trạng thái `SM-01`..`09`, bổ sung đủ `AC` cho `TP-FR-0701`, `0702`, `0901` và hiệu chỉnh cổng thoát `v0.1`.
2. **[`TP-IMP-0002`](./units/TP-IMP-0002.md)** *(Tuần tự #2, ngay sau `0001`)* — Đóng đặc tả Lô B: Hoàn thiện `runtime.proto` biên dịch sạch, phong bì `blob_ref` đa phương thức, đầy đủ `51` endpoint `OpenAPI 3.1` và chuẩn hóa cây lệnh CLI / metric / mã lỗi.
3. **[`TP-IMP-0003`](./units/TP-IMP-0003.md)** *(Tuần tự #3, ngay sau `0002`)* — Đóng đặc tả Lô C: Hoàn thiện DDL `8` bảng còn thiếu, `Project DEK`, khóa tổ hợp `(org_id, id)` + `FORCE RLS`, chuỗi băm `audit_events` theo `org_id`/JCS và lược đồ khóa Valkey `{org_id}`.
4. **[`TP-IMP-0004`](./units/TP-IMP-0004.md)** *(Song song Làn A ngay sau `0003`)* — Chạy 4 thử nghiệm có bấm giờ `SPIKE-01`..`04` và ghi biên bản khóa cứng chuẩn `Wasm ABI` (`POC-01`).
5. **[`TP-IMP-0005`](./units/TP-IMP-0005.md)** *(Song song Làn B ngay sau `0003`)* — Khởi tạo cấu trúc Monorepo (`cmd/`, `internal/`, `web/console/`), đồng bộ giấy phép `Apache-2.0` (`ISS-002`), dựng chốt chặn `CONSTRAINTS.md` Floor Guard và đường ống CI `Stage 0/1`.

---

## 3. Điều kiện Tiền đề Môi trường, Dữ liệu Kiểm thử Khép kín & Quy trình Dựng / Đóng gói (`Env, Test Data & Packaging Procedures`)

1. **Điều kiện Tiền đề Máy Trạm Kỹ sư & CI Runner (`Environmental Prerequisites`):**
   - **Go Toolchain:** `Go >= 1.24` (`module github.com/nguywnben/tanpopo`). **Lưu ý cờ biên dịch bắt buộc:** Trình phát hiện tranh chấp `-race` của Go yêu cầu `CGO_ENABLED=1` (cần trình biên dịch C trên máy trạm/CI), do đó **tuyệt đối không bao giờ gộp `CGO_ENABLED=0` và `-race` trên cùng một dòng lệnh** — luôn tách làm 2 bước: (a) kiểm tra thuần Go không C: `CGO_ENABLED=0 go test ./...` (PowerShell: `$env:CGO_ENABLED='0'; go test ./...`), và (b) kiểm tra tranh chấp dữ liệu: `go test -race ./...` (hoặc gọi trình chạy đa nền tảng `go run ./scripts/ci/check.go`).
   - **Node.js & UI Toolchain:** `Node.js >= 22.13.0`, `npm` (luôn cài bằng `npm ci --ignore-scripts`), gọi Astryx CLI qua `npx @astryxdesign/cli ...` (trên Windows PowerShell dùng `cmd /c npx @astryxdesign/cli ...` theo `ISS-006`).
   - **Công cụ Hợp đồng & Bảo mật:** `buf` / `protoc`, `golangci-lint`, `govulncheck`, `osv-scanner`, `gitleaks`, `cosign`, `syft`/`cyclonedx-gomod`, và Docker/Compose cục bộ (chỉ cần cho kiểm thử tích hợp `PostgreSQL 16` + `Valkey 8` ở `Stage 2 CI` và `TP-IMP-0039+`).
   - **Mẫu Wasm Kiểm thử Đóng Sẵn (`Zero Host TinyGo Requirement for Unit Tests`):** Các mô-đun `.wasm` nhỏ phục vụ kiểm thử đối kháng (`infinite_loop.wasm`, `oom_alloc.wasm`, `unreachable_trap.wasm`, `unapproved_wasi.wasm`, `echo_transform.wasm`) được dựng sẵn từ mã nguồn `.wat` minh bạch lưu trong `internal/testkit/wasmfixtures/` để `go test` chạy được ngay trên mọi máy Windows/macOS/Linux mà không bắt buộc cài trước `TinyGo`; `TinyGo` / `Rust wasm32` chỉ cần khi tác giả biên dịch gói mẫu SDK mới.
2. **Quy trình Dữ liệu Kiểm thử Khép kín (`Hermetic Test Data & Zero External AI Cost`):**
   - Mọi gói kiểm thử bắt buộc đặt `TANPOPO_TEST_HERMETIC=1` ngay trong `TestMain`, kích hoạt `testkit/netguard` đánh trượt tức thì mọi kết nối socket ra ngoài `127.0.0.0/8` / `::1`.
   - Mọi thông tin xác thực trong bộ kiểm thử dùng chuỗi mồi nhử chuẩn `sk-tp-canary-DO-NOT-LEAK-9f8e7d6c` để `CanaryScanner` quét hậu kiểm trên toàn bộ nhật ký, DB, OTel spans và `support-bundle`.
   - Mỗi ca kiểm thử tạo một thư mục `t.TempDir()` (với SQLite) hoặc một `org_id` `UUIDv7` ngẫu nhiên có hạt giống `TANPOPO_TEST_SEED` (với PostgreSQL/Valkey) để chạy `t.Parallel()` an toàn tuyệt đối.
3. **Quy trình Biên dịch Nhị phân Đơn & Đóng gói Xuất xưởng (`Build & Package Procedure`):**
   - Bước 1 *(Web Console Static Build)*: `cd web/console && npm ci --ignore-scripts && npm run build && npx @astryxdesign/cli doctor --built dist` (`0 fail`).
   - Bước 2 *(Embed & Pure-Go Cross-Compile)*: Sao chép/liên kết tài sản tĩnh vào gói nhúng `internal/webui/dist`, chạy `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -mod=readonly -trimpath -ldflags="-s -w -X main.version=<git-tag>" -o dist/tanpopo ./cmd/tanpopo`.
   - Bước 3 *(Provenance & Signing)*: Sinh tệp băm `SHA256SUMS`, xuất `sbom.cdx.json`, ký không khóa `cosign sign-blob` và chạy kiểm thử khói ngoại tuyến trên nhị phân vừa tạo trước khi trình duyệt cổng phát hành.
