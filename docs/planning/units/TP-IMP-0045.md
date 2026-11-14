# `TP-IMP-0045` — Chuỗi Công cụ Đóng gói Mạng Kín `Air-Gap Bundle` (`bundle export/verify/import`) & Mở khóa Giao diện Quản trị Đa Tổ chức `Multi-Org GA`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0045` |
| **Milestone / Epic** | `M5` (`v1.0 GA Hardening, Docs & Release`) · `EP-09` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0035`, `TP-IMP-0040`, `TP-IMP-0041` — `GAP-01, 12`)* |
| **Quy mô (`Size`)** | `M` (`internal/cli/bundle/*`, `web/console/src/shell/OrgSwitcher.tsx`, `web/console/src/pages/orgs/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Xác minh chữ ký chuỗi cung ứng ngoại tuyến `CT-31` & ranh giới chuyển đổi ngữ cảnh Đa Tổ chức trên giao diện)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Enterprise Platform & Frontend Lead` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hoàn thiện hai năng lực triển khai quy mô lớn được quy hoạch cho `v1.0 GA` theo [`release-scope.md`](../../product/release-scope.md), [`installer-upgrades.md`](../../operations/installer-upgrades.md) và `CR-04`:
1. **Chuỗi Công cụ Mạng Kín Cách ly Hoàn toàn (`Air-Gap Offline Bundle` — `tanpopo bundle export / verify / import`, `CT-31`, `OPS-AC-11`, `GATE-REL-06`):** Cho phép đội hạ tầng tại vùng có mạng đóng gói nhị phân/ảnh container, danh mục các gói `.tpkg` đã ghim, tệp `index.json` + `revocations.json`, khóa công khai gốc `Trust Root` và `SBOM` vào một tệp `.tpbundle` duy nhất có ký số; đem qua cổng kiểm soát vào mạng quân sự/tài chính **không có bất kỳ kết nối Internet hay DNS ngoài nào** để xác minh chữ ký ngoại tuyến và cài đặt chạy trọn vẹn `100%`.
2. **Mở khóa Quản trị Đa Tổ chức Đầy đủ Trên Cùng Một Cụm (`Multi-Organization Console UX` — `BA-OQ-001`):** Kích hoạt bộ chuyển đổi `Organization Switcher` trên thanh đỉnh `AppShell` (chỉ hiện khi người dùng có quyền trên `> 1` tổ chức hoặc mang vai trò chuẩn `OPERATOR` cấp máy chủ; đối với cài đặt 1 tổ chức mặc định vẫn giữ giao diện tinh gọn), xóa sạch toàn bộ bộ nhớ đệm truy vấn trên trình duyệt ngay khoảnh khắc chuyển đổi `Org`!

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/cli/bundle/export.go`, `verify.go`, `import.go` (đóng gói, xác minh chữ ký ngoại tuyến không chạm mạng và nạp kho phần mở rộng cục bộ).
  - `web/console/src/shell/OrgSwitcher.tsx` & `web/console/src/pages/orgs/OrganizationsAdminPage.tsx` (khởi tạo từ Astryx template, tự động `queryClient.clear()` và đặt lại ngữ cảnh `Project / Env` khi đổi `Org`).
  - Bài kiểm thử tích hợp container tắt hẳn card mạng (`docker run --network=none`) xác nhận cài đặt, mở Web Console (với phông chữ `Figtree` nội bộ từ `TP-IMP-0026`) và chạy suy luận với máy chủ mô hình nội bộ thành công.
- **Out-of-Scope:** Không phát sinh tính năng mới ngoài phạm vi `v1.0 GA`.

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0307`, `TP-FR-0501`, `BA-OQ-001`, `CT-31`, `OPS-AC-11`, `UX-AC-10`, `CR-04`.
- **Dependencies:** `TP-IMP-0035`, `TP-IMP-0040`, `TP-IMP-0041`.
- **Design Refs:** [`installer-upgrades.md`](../../operations/installer-upgrades.md) (`SPEC-OPS-01` Mục 6), [`information-architecture.md`](../../ux/information-architecture.md) (`SPEC-UX-01`), [`contract-test-matrix.md`](../../quality/contract-test-matrix.md) (`CT-31`).

## 4. API, Wire & Data Contracts
- **Bất biến Xác minh Ngoại tuyến 100% (`CT-31`):** `tanpopo bundle verify` và `tanpopo bundle import` chạy dưới cờ cấm mở socket mạng — mọi chuỗi chứng chỉ/khóa công khai cần thiết để xác minh chữ ký và `revocations.json` đều tự chứa trong tệp `.tpbundle` đối chiếu với khóa gốc tin cậy cài trên máy đích.
- **Cô lập Trạng thái Giao diện Khi Đổi `Org`:** Ngay khi chuyển từ `Org A` sang `Org B`, toàn bộ bộ nhớ đệm dữ liệu phía trình duyệt, URL ngữ cảnh và khóa phiên `Playground` tạm thời của `Org A` đều bị hủy sạch ngay lập tức.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1` (`CT-31` & `OPS-AC-11` True Zero-Egress Air-Gap Install):** Trong môi trường cô lập hoàn toàn không có tuyến ra ngoài, chạy `tanpopo bundle verify ./release.tpbundle && tanpopo bundle import ./release.tpbundle` → khởi động Gateway, mở Web Console trên trình duyệt (`0` lỗi tải phông chữ/tài nguyên ngoài) và thực hiện suy luận nội bộ thành công `100%`.
- **`AC-2` (`Multi-Org Progressive Disclosure`):** Với cài đặt 1 `Org`, thanh điều hướng giữ nguyên sự tối giản; với người dùng thuộc `Org A` và `Org B`, chuyển qua lại giữa 2 tổ chức hiển thị đúng tài nguyên tương ứng với `0` rò rỉ dữ liệu đệm trên màn hình.
- **`NEG-1` (`Tampered Air-Gap Bundle`):** Sửa 1 byte của một gói `.tpkg` bên trong tệp `.tpbundle` → `tanpopo bundle verify` phát hiện sai lệch chữ ký/mã băm ngay lập tức và từ chối nhập gói (`exit code 22`).
- **`NEG-2` (`UX-AC-10`):** Chạy trong môi trường mạng tắt hoàn toàn xác nhận `0` yêu cầu trình duyệt ra ngoài và `npx @astryxdesign/cli doctor --built dist` đạt `0 fail`.

## 6. Test Commands & Required Evidence
- Thuần Go (`CGO_ENABLED=0`): `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v ./internal/cli/bundle/...` *(PowerShell: `$env:CGO_ENABLED='0'; $env:TANPOPO_TEST_HERMETIC='1'; go test -v ./internal/cli/bundle/...`)*
- Tranh chấp dữ liệu (`-race`): `go test -v -race ./internal/cli/bundle/...`
- `cd web/console && npm run typecheck && npm run test && npm run build && npx astryx doctor --built dist`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Quyền tạo mới `Organization` trên cùng một cụm chỉ dành cho vai trò chuẩn `OPERATOR` cấp máy chủ (hoặc cấu hình khai báo), ngăn người dùng một tổ chức tự ý sinh thêm tổ chức rác.
- **Rollback:** Có thể khóa cụm ở chế độ đơn tổ chức qua cấu hình `tenancy.multi_org_enabled = false`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0035`, `TP-IMP-0040` và `TP-IMP-0041` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca `CT-31`, `OPS-AC-11`, `UX-AC-10`, `astryx doctor --built dist` đạt `0 fail`, vượt qua `REQUIRES_STRONG_REVIEW`.
