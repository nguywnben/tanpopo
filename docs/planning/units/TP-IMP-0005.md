# `TP-IMP-0005` — Khởi tạo Cấu trúc Monorepo, Đồng bộ Giấy phép `Apache-2.0` (`ISS-002`), `.gitignore`, `CONSTRAINTS.md` & Khung CI Đa nền tảng `Stage 0/1`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0005` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0003` & phê duyệt `ADR-0001, ADR-0004` — `GAP-01`)* |
| **Quy mô (`Size`)** | `M` (`go.mod`, `.gitignore`, `web/console/`, `CONSTRAINTS.md`, `scripts/ci/check.go` & GitHub Actions) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Thiết lập chốt chặn chất lượng `Floor Guard`, đường dẫn mô-đun Go gốc & cấu trúc kho mã)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Platform Engineer + CI/Security Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Thiết lập nền móng vật lý sạch sẽ và chạy đồng nhất trên cả **Linux, macOS lẫn Windows PowerShell** theo đúng [`ADR-0001`](../../architecture/adr/0001-core-runtime-and-modular-monolith.md) và [`ADR-0004`](../../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md):
1. Khởi tạo `go.mod` gốc với tên mô-đun chuẩn duy nhất **`module github.com/nguywnben/tanpopo`** (`go 1.24.0`).
2. Dời các tệp giao diện sơ khai ở gốc (`package.json`, `package-lock.json`, `astryx-themes.js`, `src/*`) về `web/console/` mà bảo toàn nguyên vẹn theme `neutral` (`ISS-002`..`004`: đổi `"license": "Apache-2.0"`, khai báo tường minh `react@19.3.1` & `react-dom@19.3.1`, chuyển `@astryxdesign/cli` sang `devDependencies`, đặt `"type": "module"`).
3. Cứng hóa `.gitignore` (chặn nhị phân `/tanpopo`, `*.exe`, tệp SQLite `*.db*`, khóa `*.key`/`*.pem`, bản sao lưu `*.tpop.enc`, `.worktrees/`) và xây dựng trình kiểm định chất lượng thuần Go đa nền tảng **`scripts/ci/check.go`** thực thi `CONSTRAINTS.md` (`Floor Guard`, kiểm tra bản quyền `Apache-2.0`, kiểm tra chiều phụ thuộc gói) không phụ thuộc `bash`.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod` (`module github.com/nguywnben/tanpopo`), `cmd/tanpopo/main.go` (khung khởi tạo trả `--version`).
  - Khung thư mục gói chuẩn kèm tệp `doc.go` định nghĩa ranh giới chiều phụ thuộc: `internal/{pkg,testkit,storage,config,security,iam,extension,provider,policy,ratelimit,routing,finops,telemetry,webhook,ingress,admin,webui,cli,ops}`.
  - `.gitignore` (bổ sung đầy đủ mẫu loại trừ cho Go, SQLite WAL, tệp khóa/sao lưu và `git worktree`).
  - Dời `package.json`, `package-lock.json`, `astryx-themes.js`, `src/*` vào `web/console/` và cập nhật ghi chú đường dẫn trong `AGENTS.md` (giữ nguyên vẹn 100% khối `<!-- ASTRYX:START -->..<!-- ASTRYX:END -->` và khối `<!-- TANPOPO-HANDOFF:START -->..<!-- TANPOPO-HANDOFF:END -->`).
  - `CONSTRAINTS.md`, `scripts/ci/check.go` (chạy được bằng `go run ./scripts/ci/check.go` trên cả Windows/macOS/Linux) và `.github/workflows/ci.yml` (`Stage 0` & `Stage 1`, đặt sẵn biến mồi nhử `OPENAI_API_KEY=CANARY_FORBIDDEN_CI_KEY`).
- **Out-of-Scope:** Cấu hình Vite/TypeScript (`tsconfig.json`, `tsc --noEmit`) và dựng màn hình giao diện thật thuộc `TP-IMP-0026` (`Stage 1` chỉ bật cổng `npm run typecheck` ngay khi `web/console/tsconfig.json` xuất hiện ở `TP-IMP-0026`); chưa viết logic lưu trữ hay mạng (thuộc `TP-IMP-0006`+).

## 3. Requirements, Dependencies & Design References
- **Requirements & Issues Refs:** `TP-NFR-0901`, `TP-NFR-1001`, `ISS-002`..`004`, `RSK-012`, `GATE-REL-01`, `GATE-REL-04`.
- **Dependencies:** `TP-IMP-0003`.
- **Design Refs:** [`ADR-0001`](../../architecture/adr/0001-core-runtime-and-modular-monolith.md), [`ADR-0004`](../../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md), [`release-readiness.md`](../../quality/release-readiness.md) Mục 2, [`test-strategy.md`](../../quality/test-strategy.md) Mục 5, [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Mục 1.1.

## 4. API, Wire & Data Contracts
- **Thứ tự Phụ thuộc Gói Một Chiều (`One-Way Package DAG` kiểm tra bởi `scripts/ci/check.go`):**
  `pkg` ← `storage/port` ← `storage/{sqlite,postgres,migrate}` ← `security/{vault,egress,broker,oauth,audit,killswitch,foureyes}` ← `config/snapshot` ← `{iam,extension,provider,policy,ratelimit,routing,finops,telemetry,webhook}` ← `{ingress,admin,webui,ops}` ← `cli` ← `cmd/tanpopo` (`0` vòng lặp import; cấm tuyệt đối import `tools/spikes` hay `http.DefaultClient`).
- **`CONSTRAINTS.md` & `Floor Guard`:** Đánh trượt tự động mọi PR chứa: (a) `.skip` / `t.Skip()` không gắn mã ticket, (b) `//nolint:gosec`, `@ts-ignore`, `@ts-expect-error`, `eslint-disable`, `# gitleaks:allow`, (c) thẻ `div`/`span` thô trong `web/console/src`, (d) thư viện phụ thuộc mang giấy phép `GPL`/`AGPL`/`SSPL` (riêng tệp siêu dữ liệu công cụ `.agents/plugin.json` mang giấy phép gốc `MIT` của bộ skills được liệt kê rõ trong danh sách cho phép tương thích `Apache-2.0`), hoặc (e) tệp khóa thứ hai cạnh tranh (`yarn.lock` / `pnpm-lock.yaml`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given cấu trúc kho mã mới, When chạy `go run ./scripts/ci/check.go --pure-go-build` và kiểm tra `npx @astryxdesign/cli doctor` bên trong `web/console/`, Then biên dịch nhị phân `tanpopo` thành công (`CGO_ENABLED=0`) và `astryx doctor` đạt `0 fail` với `defaultThemeSlug = "neutral"` giữ nguyên `100%`.
- **`AC-2`:** Given `LICENSE` và `web/console/package.json`, When chạy `go run ./scripts/ci/check.go --license`, Then toàn bộ mã nguồn dự án đồng nhất giấy phép `Apache-2.0` (`ISS-002` được đóng hoàn toàn) và `0` gói phụ thuộc dính copyleft cấm.
- **`AC-3`:** Given `.github/workflows/ci.yml` chạy trên cả Linux runner (dùng `npx @astryxdesign/cli doctor`) và kiểm thử cục bộ trên Windows PowerShell (dùng `cmd /c npx @astryxdesign/cli doctor`), Then `Stage 0` & `Stage 1` hoàn tất dưới `< 3 phút`.
- **`NEG-1`:** Cố tình chèn 1 dòng `@ts-ignore`, `//nolint:gosec`, `http.DefaultClient` hoặc `t.Skip()` giả lập vào bài test của `check.go` → trả mã thoát khác `0`.
- **`NEG-2`:** Cố tình chạy `astryx theme eject` hoặc sửa tay phần nội dung sinh tự động của `astryx-themes.js` → bị cấm tuyệt đối.
- **`NEG-3`:** Không cho phép `npm ci` tự động thực thi kịch bản `postinstall` chưa duyệt (`--ignore-scripts` là bắt buộc).

## 6. Test Commands & Required Evidence
- Đa nền tảng (chạy giống hệt trên cả Windows PowerShell, macOS và Linux):
  - `go run ./scripts/ci/check.go --all`
  - `go test -v ./...` *(và `go test -v -race ./...` trên môi trường có C toolchain)*
  - Trong `web/console`: `npm ci --ignore-scripts` + `npx @astryxdesign/cli doctor` *(Windows PowerShell: `cmd /c npx @astryxdesign/cli doctor`)*.

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Khóa cứng chuỗi cung ứng nội bộ ngay từ commit code đầu tiên (`go.sum` readonly + `npm ci --ignore-scripts` + `gitleaks` + `.gitignore` chặn tệp khóa/DB).
- **Rollback:** Một PR di chuyển cấu trúc nguyên tử chưa chứa dữ liệu người dùng; có thể `git revert` hoàn toàn về mốc tài liệu nếu cần.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Product Owner đã phê duyệt `ADR-0001`, `ADR-0004`, `SD-G1-05` (`Apache-2.0`) và hoàn tất `TP-IMP-0003`.
- **DoD:** Đóng `ISS-002`, `ISS-003`, `ISS-004`; `go run ./scripts/ci/check.go --all` và `Stage 0/1 CI` xanh `100%`; đạt phê duyệt `REQUIRES_STRONG_REVIEW`.
