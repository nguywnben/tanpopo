# Sổ Đăng ký Quyết định Kiến trúc (Architecture Decision Records — ADR Index) — Tanpopo

> **Tài liệu Quản trị Kiến trúc (Phase ASTRA A05 — Gate G3: `READY_FOR_REVIEW`)**
> - **Ngày cập nhật:** `2026-11-11`
> - **Vòng đời trạng thái chuẩn:** `PROPOSED` → `ACCEPTED` (chỉ khi Product Owner phê duyệt tường minh) → `SUPERSEDED BY ADR-XXXX` / `DEPRECATED`.
> - **Kỷ luật bất biến:** Không xóa các ADR cũ; mọi thay đổi kiến trúc lớn về sau phải tạo ADR mới tham chiếu ngược về ADR bị thay thế.

---

## Danh mục Các Quyết định Kiến trúc Có Hậu quả Lớn Trình Duyệt Tại Gate G3

| Mã ADR | Tiêu đề quyết định kiến trúc | Trạng thái | Mức độ đảo ngược (`Reversibility`) | Liên kết `OQ` / `POC` / `RSK` |
|---|---|---|---|---|
| [**`ADR-0001`**](./0001-core-runtime-and-modular-monolith.md) | Chọn **Go (`1.24+`) Modular Monolith** hỗ trợ tách vai trò (`--role`) cho Tanpopo Core & CLI | **`PROPOSED`** | **Khó đảo ngược** (*One-Way Door* sau khi viết Core) | `OQ-001`, `TP-NFR-0101`..`0103`, `TP-NFR-0901` |
| [**`ADR-0002`**](./0002-three-tier-extension-sandbox-and-blind-broker.md) | Mô hình Thực thi Phần mở rộng 3 Tầng (`Declarative` + `Pure-Go Wasm Sandbox` + `ConnectRPC Sidecar`) & `Blind Credential Broker` | **`PROPOSED`** | **Trung bình – Khó** (ABI/Manifest trở thành cam kết hệ sinh thái ở `v1.0`) | `OQ-002`, `SD-G1-02`, `POC-01`..`03`, `RSK-001`, `RSK-004` |
| [**`ADR-0003`**](./0003-dual-mode-persistence-and-cluster-state.md) | Lưu trữ Hai Chế độ qua Giao diện Trừu tượng: **SQLite WAL** (Đơn nút) & **PostgreSQL `16+` + Valkey/Redis** (Cụm HA) | **`PROPOSED`** | **Trung bình** (Đã cách ly sau `storage/port`) | `OQ-004`, `SD-G1-01`, `TD-G2-06`, `RSK-013` |
| [**`ADR-0004`**](./0004-web-console-stack-astryx-evaluation-and-repo-layout.md) | Đánh giá Độc lập Stack Web Console: **SPA Tĩnh `TypeScript + React 19 + Vite + StyleX + Astryx (neutral)`** nhúng vào binary Go & Quy hoạch Monorepo Apache-2.0 | **`PROPOSED`** | **Dễ – Trung bình** (Tách rời qua REST API `/admin/v1/*`) | `OQ-007`, `DEC-007`, `ASM-003`, `ISS-001`..`005`, `RSK-012` |
| [**`ADR-0005`**](./0005-two-phase-budget-ledger-and-commercial-billing-separation.md) | Sổ cái Ngân sách Hai Pha Số nguyên `Micro-USD`, Gắn nhãn Độ chính xác & Tách biệt Tuyệt đối Thanh toán API Thương mại Khỏi Thuê bao Cá nhân | **`PROPOSED`** | **Khó đảo ngược** (Liên quan trực tiếp toàn vẹn sổ cái tài chính) | `DEC-004`, `REJ-003`, `TD-G2-03`, `BA-OQ-002`, `RSK-007` |
| [**`ADR-0006`**](./0006-immutable-config-snapshots-and-fast-revocation.md) | Ảnh chụp Cấu hình Bất biến (`Copy-on-Write Snapshot`), Truyền Tin Thu hồi Kép (`< 1s` / `< 5s`) & Chế độ Suy giảm Phân tầng | **`PROPOSED`** | **Dễ đảo ngược nội bộ** (Không làm đổi API bên ngoài) | `BA-OQ-003`, `TP-NFR-0404`, `RSK-011` |
