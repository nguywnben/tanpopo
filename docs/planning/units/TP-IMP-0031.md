# `TP-IMP-0031` — Công cụ Dòng lệnh Hợp nhất `tanpopo` CLI, Nhúng SPA Tĩnh `embed.FS`, Cứu hộ `reset-local-owner` & Gói Chẩn đoán `support-bundle`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0031` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-05` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0014`, `TP-IMP-0025`, `TP-IMP-0027`, `TP-IMP-0028`, `TP-IMP-0029`, `TP-IMP-0030` — `GAP-01, 13`)* |
| **Quy mô (`Size`)** | `M` (`cmd/tanpopo/*`, `internal/cli/*`, `internal/webui/embed.go`) |
| **Cấp độ Review** | `STANDARD_REVIEW` *(Một nhị phân duy nhất `tanpopo`, cứu hộ ngoại tuyến & lọc sạch bí mật gói chẩn đoán `OPS-AC-10`)* |
| **Chủ sở hữu Kỹ thuật** | `Developer Experience & Systems Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hợp nhất toàn bộ sản phẩm vào **một tệp thực thi duy nhất `tanpopo`** (`ADR-0001`, `CR-10` — xóa bỏ hoàn toàn sự phân mảnh tên gọi `tanpopoctl`):
1. Nhúng bản dựng tĩnh `web/console/dist` vào nhị phân Go qua `embed.FS` để lệnh `tanpopo serve` phục vụ ngay cả Data Plane, Admin API lẫn Web Console mà không cần cài Node.js hay Nginx lúc chạy (`TP-FR-1001`).
2. Hoàn thiện cây lệnh quản trị từ dòng lệnh (`tanpopo serve`, `config`, `provider`, `model`, `route`, `key`, `plugin`, `budget inspect` phục vụ sổ tay `INC-01`, `audit`, `killswitch`) hỗ trợ cờ `--output json|table|yaml`, bảng mã thoát chuẩn `0 / 2 / 10 / 14 / 22 / 30` của `SPEC-OPS-04` và **bộ lọc loại bỏ ký tự điều khiển đầu cuối nguy hiểm `ANSI / OSC-52 clipboard escape` (`\x1b]52;...`) trên mọi bảng in ra màn hình (`CT-27`)**.
3. Bổ sung lệnh cứu hộ khẩn cấp truy cập trực tiếp ổ đĩa cục bộ **`tanpopo admin reset-local-owner`** khi quản trị viên quên mật khẩu trong chế độ đơn nút không có SMTP (`CR-09`) và lệnh đóng gói chẩn đoán tự che sạch bí mật **`tanpopo support-bundle`** (`OPS-AC-10`).

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `cmd/tanpopo/main.go` (điểm vào duy nhất hỗ trợ `--role=all|data-plane|control-plane`).
  - `internal/webui/embed.go` (phục vụ tài nguyên tĩnh nén sẵn với `Cache-Control: immutable` cho tệp băm và fallback SPA cho các đường dẫn giao diện + tiêu đề bảo mật `CSP`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` — đáp ứng đo Lighthouse CI `LCP <= 2.0s`, `INP <= 150ms`, `CLS <= 0.05` theo `UX-AC-08`).
  - `internal/cli/root.go`, `internal/cli/sanitize/ansi.go` (`CT-27` lọc sạch chuỗi thoát ANSI/OSC-52) & các nhóm lệnh con quản trị từ xa/cục bộ (bao gồm `tanpopo budget inspect`).
  - `internal/cli/admin/reset_owner.go` (`tanpopo admin reset-local-owner`: bắt buộc chứng minh quyền sở hữu máy chủ cục bộ bằng cách kiểm tra quyền đọc tệp `Master KEK` và ghi sự kiện kiểm toán `LOCAL_BREAK_GLASS_OWNER_RESET` vào chuỗi `"SYSTEM"`, thu hồi toàn bộ phiên trình duyệt cũ).
  - `internal/cli/support/bundle.go` (`tanpopo support-bundle`: thu thập cấu hình đã che mật, trạng thái sức khỏe, danh sách gói `tanpopo.lock`, chỉ số Prometheus và nhật ký gần nhất đi qua `privacy.Redactor` + kiểm tra tự động `CanaryScanner` ngay trước khi xuất tệp `.tar.gz`).
- **Out-of-Scope:** Các lệnh `tanpopo bundle export/verify/import` cho kho gói mạng kín quy mô lớn (thuộc `TP-IMP-0045`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0903`, `TP-FR-1001`, `TP-NFR-1002`, `ADR-0001`, `ADR-0004`, `CT-27`, `UX-AC-08`, `OPS-AC-10`, `INC-01`, `CR-09`, `CR-10`, `GAP-13`.
- **Dependencies:** `TP-IMP-0014`, `TP-IMP-0025`, `TP-IMP-0027`, `TP-IMP-0028`, `TP-IMP-0029`, `TP-IMP-0030`.
- **Design Refs:** [`developer-experience.md`](../../operations/developer-experience.md) (`SPEC-OPS-04`), [`ADR-0001`](../../architecture/adr/0001-core-runtime-and-modular-monolith.md), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-09`, `CR-10`).

## 4. API, Wire & Data Contracts
- **Chuẩn Đầu Ra Tự động hóa:** Khi truyền `--output json`, `stdout` chỉ chứa đúng 1 đối tượng/mảng JSON hợp lệ (mọi dòng tiến trình/cảnh báo đẩy sang `stderr`) để kịch bản `jq` không bao giờ vỡ cú pháp.
- **Bảo vệ Lệnh Cứu hộ `reset-local-owner`:** Chỉ hoạt động trực tiếp qua dòng lệnh nội bộ trên máy chủ nắm giữ tệp khóa/CSDL — **tuyệt đối không mở thành điểm cuối HTTP từ xa trên `/admin/v1/*`**.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`Single Binary Self-Host`):** Sao chép duy nhất 1 tệp nhị phân `tanpopo` sang một thư mục trống hoàn toàn (không có `node_modules`, không có thư mục `web/`), chạy `tanpopo serve` → cả `/v1/*`, `/admin/v1/*` và giao diện Web Console hoạt động đầy đủ `100%`.
- **`AC-2` (`CR-09` Break-Glass Password Recovery):** Khi quên mật khẩu `ORG_OWNER`, chạy `tanpopo admin reset-local-owner --db ...` sinh mật khẩu khôi phục một lần, vô hiệu hóa toàn bộ phiên cũ và ghi rõ bút toán `LOCAL_BREAK_GLASS_OWNER_RESET` vào chuỗi băm kiểm toán `"SYSTEM"`.
- **`AC-3` (`OPS-AC-10` Zero-Leak Support Bundle):** Nạp các khóa mồi `sk-tp-canary-...`, `TANPOPO_MASTER_KEY` và `vk_tp_live_...`, chạy `tanpopo support-bundle -o diag.tar.gz` → giải nén và quét toàn bộ tệp bên trong xác nhận **`0` byte bí mật rò rỉ**.
- **`NEG-1` (`CT-27` CLI Terminal Escape Injection & Unauthorized Reset):** Dữ liệu tên gói/nhật ký chứa chuỗi độc hại `\x1b]52;c;...` khi in qua `tanpopo` CLI ở chế độ bảng bị lột sạch ký tự điều khiển `ESC`; gọi `tanpopo admin reset-local-owner` từ tài khoản không có quyền đọc tệp `Master KEK` bị từ chối mã thoát `14`.

## 6. Test Commands & Required Evidence
- Dựng SPA tĩnh: `cd web/console && npm run build`
- Thuần Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./cmd/tanpopo/... ./internal/cli/... ./internal/webui/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./cmd/tanpopo/... ./internal/cli/... ./internal/webui/...`)*
- Tranh chấp dữ liệu (`-race`): `go test -v -race ./internal/cli/... ./internal/webui/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Tệp `diag.tar.gz` được tạo với quyền `0600` (chỉ chủ sở hữu đọc được) và tự kiểm tra lại bằng bộ dò mẫu bí mật trước khi báo thành công.
- **Rollback:** Lùi phiên bản nhị phân `tanpopo` đơn giản trong 1 thao tác thay tệp.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0014`, `TP-IMP-0025` và toàn bộ các gói giao diện `M2` (`0027`..`0030`) đã hoàn tất.
- **DoD:** Nhị phân tĩnh chạy độc lập sạch lỗi, xanh ca `OPS-AC-10` và cứu hộ `reset-local-owner`.
