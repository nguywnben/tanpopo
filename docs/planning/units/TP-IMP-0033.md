# `TP-IMP-0033` — Đóng gói Đa Nền tảng (`linux`, `darwin`, `windows`), Ảnh OCI Không Đặc quyền, Dịch vụ `systemd`/Compose, Xuất `SBOM` & Ký `Cosign`

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0033` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product` — **Cổng `GATE-M2`**) · `EP-07` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0032` — `GAP-01`)* |
| **Quy mô (`Size`)** | `M` (`deploy/docker/*`, `deploy/compose/*`, `deploy/systemd/*`, `.goreleaser.yaml`, `scripts/release/verify-artifacts.sh`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Chuỗi cung ứng phần mềm cốt lõi, container không đặc quyền `nonroot` & cổng thoát `MVP v0.1`)* |
| **Chủ sở hữu Kỹ thuật** | `Release & Platform Security Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Đóng gói toàn bộ sản phẩm `MVP (v0.1)` thành các hiện vật phân phối tái lập được (`Reproducible Builds`), an toàn chuỗi cung ứng và chạy ngay trên cả 3 hệ điều hành mục tiêu (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`), bao gồm ảnh container OCI tối giản không quyền root (`distroless/static:nonroot`, hệ tệp gốc chỉ đọc `readOnlyRootFilesystem`), bộ mẫu `docker-compose.yml` + `tanpopo.service` (`systemd` hardening), sinh tự động danh mục linh kiện `CycloneDX SBOM` cho cả Go lẫn npm và ký số `Cosign` + `SHA256SUMS` — khép kín **Cổng `GATE-M2` (`MVP v0.1`)**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `.goreleaser.yaml` (biên dịch chéo `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`, đóng gói `.tar.gz` / `.zip` tái lập được cho 5 tổ hợp OS/Arch).
  - `deploy/docker/Dockerfile` (đa tầng: dựng `web/console` với `--ignore-scripts` → dựng nhị phân tĩnh `tanpopo` → chép vào ảnh nền `nonroot` `UID 65532`, không có `/bin/sh` hay trình quản lý gói để triệt tiêu bề mặt tấn công hậu khai thác).
  - `deploy/compose/docker-compose.standalone.yml` (cấu hình `read_only: true`, `cap_drop: ["ALL"]`, `security_opt: ["no-new-privileges:true"]`, volume riêng cho `/var/lib/tanpopo`).
  - `deploy/systemd/tanpopo.service` (`DynamicUser=yes` hoặc user riêng, `ProtectSystem=strict`, `ProtectHome=yes`, `NoNewPrivileges=yes`).
  - `.github/workflows/release-dry-run.yml` & `scripts/release/verify-artifacts.sh` (sinh `SBOM` CycloneDX, quét lỗ hổng `govulncheck` + `trivy` `0 Critical/High`, ký thử nghiệm bằng khóa kiểm thử `Cosign` trong CI — **tuyệt đối không tự đẩy bản phát hành công khai**).
- **Out-of-Scope:** Biểu đồ `Helm Chart` đa bản sao tách vai trò cho Kubernetes HA (thuộc `TP-IMP-0040` ở `M4`); phát hành công khai thật ra bên ngoài (chỉ thực hiện ở `TP-IMP-0047` khi có chữ ký của Product Owner).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-NFR-0103`, `TP-NFR-0901`, `SPEC-OPS-01`, `SPEC-SEC-02`, `GATE-REL-02`, `GATE-REL-04`.
- **Dependencies:** `TP-IMP-0032`.
- **Design Refs:** [`installer-upgrades.md`](../../operations/installer-upgrades.md) (`SPEC-OPS-01`), [`plugin-sandbox-and-supply-chain.md`](../../security/plugin-sandbox-and-supply-chain.md) (`SPEC-SEC-02`), [`release-readiness.md`](../../quality/release-readiness.md) (`GATE-REL-02`, `GATE-REL-04`).

## 4. API, Wire & Data Contracts
- **Ngân sách Tài nguyên Khởi động Lạnh (`TP-NFR-0103` — `TARGET / UNVERIFIED`):** Bài kiểm thử khói đo thực nghiệm nhị phân đóng gói khởi động đến trạng thái `/readyz = 200` dưới `< 2 giây` và tiêu thụ RSS trạng thái nghỉ `< 128 MiB`.
- **Bất biến Container Không Đặc quyền:** Ảnh OCI từ chối chạy nếu bị ép chạy dưới `UID 0` (hoặc mặc định luôn chạy `65532:65532`) và hoạt động hoàn hảo khi hệ tệp gốc bị gắn cờ chỉ đọc (`--read-only`).

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Chạy đóng gói thử nghiệm cục bộ (`goreleaser build --snapshot --clean`) tạo đủ 5 nhị phân cho Linux/macOS/Windows mà không cần bất kỳ trình biên dịch C (`gcc`/`clang`) nào.
- **`AC-2`:** Dựng ảnh OCI và khởi chạy với `docker run --read-only --cap-drop=ALL --security-opt=no-new-privileges:true` → `/healthz`, `/readyz`, Web Console và bài kiểm thử khói `EXT-ACCEPT-01` đều vượt qua `100%`.
- **`AC-3` (`GATE-REL-02 & 04` Supply Chain):** Sinh tệp `sbom-go.cdx.json` + `sbom-web.cdx.json`, xác minh giấy phép phụ thuộc không dính `AGPL/GPL` truyền nhiễm và xác minh chữ ký `cosign verify-blob` thành công.
- **`NEG-1`:** Thử thực thi lệnh shell `/bin/sh` bên trong ảnh container sản xuất → thất bại vì ảnh nền tĩnh không chứa shell.

## 6. Test Commands & Required Evidence
- `bash scripts/release/verify-artifacts.sh`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Workflow trong đơn vị này chỉ là `dry-run` kiểm chứng đóng gói trên PR/nhánh — **cấm cấu hình tự động tạo GitHub Release công khai hoặc đẩy thẻ chính thức khi chưa qua `TP-IMP-0047`**.
- **Rollback:** Giữ ảnh container và tệp nén gắn mã băm bất biến theo từng commit.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0032` và toàn bộ các đơn vị của `Milestone M2` (`0016`..`0032`) đã hoàn tất.
- **DoD:** Đạt toàn bộ tiêu chí Cổng `GATE-M2` (`MVP v0.1`), `0` lỗ hổng `Critical/High`, đo xác nhận thời gian khởi động & RSS thực tế, vượt qua `REQUIRES_STRONG_REVIEW`.
