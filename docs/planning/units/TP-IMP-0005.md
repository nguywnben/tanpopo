# `TP-IMP-0005` — Khởi tạo Cấu trúc Monorepo, Đồng bộ Giấy phép `Apache-2.0` (`ISS-002`), `CONSTRAINTS.md` & Khung CI `Stage 0/1`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0005` |
| **Milestone / Epic** | `M1` (`Foundation & Vertical Slice`) · `EP-01` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0003` & phê duyệt `ADR-0001, ADR-0004` — `GAP-01`)* |
| **Quy mô (`Size`)** | `M` (Tổ chức cấu trúc thư mục gốc, `go.mod`, `web/console/`, `CONSTRAINTS.md` & GitHub Actions) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Thiết lập chốt chặn chất lượng `Floor Guard` & cấu trúc kho mã gốc)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Platform Engineer + CI/Security Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Thiết lập nền móng vật lý sạch sẽ cho kho mã Tanpopo theo đúng [`ADR-0001`](../../architecture/adr/0001-core-runtime-and-modular-monolith.md) và [`ADR-0004`](../../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md): khởi tạo `go.mod` (`Go 1.24+`), dời các tệp giao diện sơ khai ở gốc về `web/console/` mà vẫn bảo toàn nguyên vẹn theme `neutral` (`ISS-001`..`005`), đồng bộ giấy phép `Apache-2.0` trên toàn bộ tệp cấu hình, ban hành hợp đồng chất lượng `CONSTRAINTS.md` và dựng cổng kiểm định tự động `Stage 0` (Floor Guard, License, Gitleaks, `--ignore-scripts`) + `Stage 1` (Lint & Static Security) trên GitHub Actions.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `go.mod`, `cmd/tanpopo/main.go` (chỉ hàm khởi tạo khung trả phiên bản `--version`), khung thư mục rỗng có kiểm tra ranh giới import một chiều `internal/{ingress,iam,policy,routing,finops,extension,security,telemetry,audit,storage}`.
  - Dời `package.json`, `package-lock.json`, `astryx-themes.js`, `src/*` vào `web/console/` (đổi `"license": "Apache-2.0"`, khai báo tường minh `react` & `react-dom`, chuyển `@astryxdesign/cli` sang `devDependencies`, thêm `"type": "module"`, cập nhật đường dẫn trong `AGENTS.md` nếu cần mà giữ nguyên khối quy tắc Astryx).
  - `CONSTRAINTS.md`, `scripts/ci/floor-guard.sh`, `.github/workflows/ci.yml` (`Stage 0` & `Stage 1`).
- **Out-of-Scope:** Chưa xây dựng màn hình giao diện thật (thuộc `TP-IMP-0026`..`0030`) và chưa viết logic lưu trữ hay mạng (thuộc `TP-IMP-0006`+).

## 3. Requirements, Dependencies & Design References
- **Requirements & Issues Refs:** `TP-NFR-0901`, `TP-NFR-1001`, `ISS-001`..`005`, `RSK-012`, `GATE-REL-04`.
- **Dependencies:** `TP-IMP-0003`.
- **Design Refs:** [`ADR-0001`](../../architecture/adr/0001-core-runtime-and-modular-monolith.md), [`ADR-0004`](../../architecture/adr/0004-web-console-stack-astryx-evaluation-and-repo-layout.md), [`release-readiness.md`](../../quality/release-readiness.md) Mục 2, [`test-strategy.md`](../../quality/test-strategy.md) Mục 5, [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) Mục 1.1.

## 4. API, Wire & Data Contracts
- **Ranh giới Phụ thuộc Gói Một Chiều:** Kiểm tra tự động bằng kịch bản lint kiến trúc đảm bảo `internal/storage` không bao giờ import ngược `internal/ingress` hay `internal/routing` (`0` vòng lặp gói).
- **`CONSTRAINTS.md` & `floor-guard`:** Đánh trượt tự động mọi PR chứa: (a) `.skip` / `t.Skip()` không gắn mã ticket, (b) `//nolint:gosec`, `@ts-ignore`, `@ts-expect-error`, `eslint-disable`, `# gitleaks:allow`, (c) thẻ `div`/`span` thô trong `web/console/src`, (d) thư viện phụ thuộc mang giấy phép `GPL`/`AGPL`/`SSPL`, hoặc (e) tệp khóa thứ hai cạnh tranh (`yarn.lock` / `pnpm-lock.yaml`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given cấu trúc kho mã mới, When chạy `CGO_ENABLED=0 go build ./cmd/tanpopo` và `cd web/console && npm ci --ignore-scripts && cmd /c npx @astryxdesign/cli doctor`, Then biên dịch nhị phân thành công và `astryx doctor` đạt `0 fail` với `defaultThemeSlug = "neutral"` giữ nguyên `100%`.
- **`AC-2`:** Given `LICENSE` và `web/console/package.json`, When chạy trình quét bản quyền của `Stage 0 CI`, Then toàn bộ dự án đồng nhất giấy phép `Apache-2.0` (`ISS-002` được đóng hoàn toàn) và `0` gói phụ thuộc dính copyleft cấm.
- **`AC-3`:** Given `.github/workflows/ci.yml`, When mở PR mới, Then `Stage 0` (`floor-guard`, `gitleaks --redact`, lockfile check) và `Stage 1` (`golangci-lint`, `govulncheck`, `osv-scanner`) chạy hoàn tất dưới ngân sách `< 3 phút`.
- **`NEG-1`:** Cố tình chèn 1 dòng `@ts-ignore`, `//nolint:gosec` hoặc `t.Skip()` giả lập vào bài test của `floor-guard` → kịch bản phải trả mã thoát khác `0`.
- **`NEG-2`:** Cố tình chạy `astryx theme eject` hoặc sửa tay phần nội dung sinh tự động của `astryx-themes.js` → bị cấm tuyệt đối.
- **`NEG-3`:** Không cho phép `npm ci` tự động thực thi kịch bản `postinstall` chưa duyệt (`--ignore-scripts` là bắt buộc).

## 6. Test Commands & Required Evidence
- `bash scripts/ci/floor-guard.sh`
- `CGO_ENABLED=0 go test -race ./...`
- `cd web/console && npm ci --ignore-scripts && cmd /c npx @astryxdesign/cli doctor`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Khóa cứng chuỗi cung ứng nội bộ ngay từ commit code đầu tiên (`go.sum` readonly + `npm ci --ignore-scripts` + `gitleaks`).
- **Rollback:** Một PR di chuyển cấu trúc nguyên tử chưa chứa dữ liệu người dùng; có thể `git revert` hoàn toàn về mốc tài liệu nếu cần.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** Product Owner đã phê duyệt `ADR-0001`, `ADR-0004`, `SD-G1-05` (`Apache-2.0`) và hoàn tất `TP-IMP-0003`.
- **DoD:** Đóng `ISS-002`, `ISS-003`, `ISS-004`; `Stage 0/1 CI` xanh `100%`; đạt phê duyệt `REQUIRES_STRONG_REVIEW`.
