# `TP-IMP-0016` — Mở rộng Điểm cuối `/v1/responses`, `/v1/embeddings`, `/v1/models`, Chống Trùng `Idempotency-Key` & Luồng `blob_ref` Đa phương thức

| Thuộc tính | Giá trị |
|---|---|
| **Unit ID** | `TP-IMP-0016` |
| **Milestone / Epic** | `M2` (`MVP v0.1 Core Product`) · `EP-03` |
| **Trạng thái (`Status`)** | `BLOCKED` *(Phụ thuộc `TP-IMP-0015` — `GAP-01, 03`)* |
| **Quy mô (`Size`)** | `L` (`internal/gateway/http/responses.go`, `embeddings.go`, `models.go`, `internal/gateway/idempotency/*`, `internal/gateway/blob/*`) |
| **Cấp độ Review** | **`REQUIRES_STRONG_REVIEW`** *(Xử lý tệp đính kèm đa phương thức `<= 32 MiB` chống SSRF/OOM & cô lập khóa chống trùng theo `Org/Project`)* |
| **Chủ sở hữu Kỹ thuật** | `Staff Gateway / Protocol Engineer` |

---

## 1. Intent (Mục tiêu Đơn vị Thực thi)
Hoàn thiện bề mặt **Unified OpenAI-Compatible API** cho `MVP (v0.1)` gồm `POST /v1/responses`, `POST /v1/embeddings`, `GET /v1/models` (lọc nghiêm ngặt theo danh sách mô hình mà `Virtual Key` hiện tại được phép dùng), thực thi cơ chế chống gửi trùng `Idempotency-Key` gắn chặt phạm vi `(org_id, project_id, vkey_id, idempotency_key)` + dấu vân tay thân yêu cầu, và hiện thực hóa đường ống truyền dữ liệu nhị phân đa phương thức qua bảng cầm nắm `blob_ref` của Host (`CR-05`) để tệp ảnh/âm thanh/PDF tới `32 MiB` **không bao giờ tràn vào trần `16 MiB` bộ nhớ tuyến tính của Wasm Sandbox**.

## 2. Scope (In-Scope / Out-of-Scope & Target Paths)
- **In-Scope (`Files Touched`):**
  - `internal/gateway/http/responses.go` (`POST /v1/responses` đồng bộ & sự kiện luồng chuẩn hóa).
  - `internal/gateway/http/embeddings.go` (`POST /v1/embeddings` kiểm tra khớp kiểu đầu vào mảng chuỗi/token và chiều véc-tơ).
  - `internal/gateway/http/models.go` (`GET /v1/models` trả danh mục `LogicalModel` được cấp quyền kèm siêu dữ liệu cờ năng lực mở rộng `x_tanpopo_capabilities`).
  - `internal/gateway/blob/store.go` (tách các phần tử `image_url` / `input_audio` / `file` `base64` lớn thành `blob_ref` lưu bộ đệm luồng có giới hạn `<= 32 MiB` tổng mỗi yêu cầu; mặc định **tắt** tự tải URL ảnh từ xa để chống SSRF, hoặc bắt buộc đi qua `SSRFGuardDialer` + giới hạn MIME/kích thước nếu chính sách dự án bật tường minh).
  - `internal/gateway/idempotency/guard.go` (lưu trạng thái `IN_FLIGHT` / phản hồi hoàn tất ngắn hạn cho yêu cầu không truyền luồng có gắn `Idempotency-Key`).
- **Out-of-Scope:** Điểm cuối `/v1/rerank` (thuộc `TP-IMP-0035` ở `v0.2` theo `CR-01`); luồng âm thanh thời gian thực hai chiều WebRTC/WebSocket (`TP-FR-0106` — `Later / Post-v1.0`).

## 3. Requirements, Dependencies & Design References
- **Requirements Refs:** `TP-FR-0101`, `TP-FR-0102`, `TP-NFR-1002`, `TP-BR-0503`, `CT-08`, `CT-09`.
- **Dependencies:** `TP-IMP-0015`.
- **Design Refs:** [`api-contracts.md`](../../specifications/api-contracts.md) (`SPEC-API-01`), [`api-protocol-compatibility.md`](../../specifications/api-protocol-compatibility.md) (`SPEC-EXT-06`), [`unresolved-gaps.md`](../../reviews/unresolved-gaps.md) (`CR-05`, `GAP-03`).

## 4. API, Wire & Data Contracts
- **Kiểm tra Năng lực Nghiêm ngặt (`No Silent Parameter Drop` — `CT-08`):** Nếu yêu cầu gửi `tools`, `response_format: json_schema`, ảnh đa phương thức hoặc `reasoning_effort` tới một mô hình không hỗ trợ năng lực tương ứng (và chính sách không bật chế độ cho phép biến đổi tường minh), Gateway **bắt buộc từ chối trước khi gọi đi** với `400 capability_not_supported` kèm tên trường bị từ chối — tuyệt đối không âm thầm cắt bỏ tham số.
- **Hợp đồng `Idempotency-Key`:**
  - Cùng `(org_id, project_id, vkey_id, key)` + cùng `body_sha256` đã hoàn tất → trả lại phản hồi đã lưu kèm header `Idempotent-Replayed: true` mà không trừ ngân sách lần hai.
  - Cùng `key` nhưng khác `body_sha256` → từ chối `422 idempotency_key_payload_mismatch`; nếu yêu cầu đầu đang `IN_FLIGHT` → trả `409 idempotency_request_in_progress`.

## 5. Acceptance Criteria & Mandatory Negative Cases
- **`AC-1`:** Given yêu cầu đa phương thức đính kèm ảnh `base64` nặng `20 MiB` đi qua Adapter Wasm Tầng 2 (`16 MiB` giới hạn RAM), When gọi `POST /v1/chat/completions` hoặc `POST /v1/responses`, Then Host tách ảnh sang `blob_ref`, Wasm chỉ biến đổi siêu dữ liệu JSON nhẹ và Host ghép luồng byte thẳng ra kết nối đích thành công mà không chạm trần bộ nhớ Wasm.
- **`AC-2`:** Given `Virtual Key` chỉ được phép gọi `gpt-4o-mini` và `text-embedding-3-small`, When gọi `GET /v1/models`, Then danh sách trả về chỉ chứa đúng 2 mô hình đó (ẩn hoàn toàn các mô hình khác của tổ chức).
- **`NEG-1` (`CT-08` Silent Parameter Drop Prevention):** Gửi `response_format: {"type": "json_schema", ...}` tới mô hình khai báo `supports_structured_output = false` → trả `400 capability_not_supported` trong `< 2 ms`, `0` cuộc gọi lên thượng nguồn.
- **`NEG-2` (`Multimodal SSRF`):** Gửi `image_url: "http://169.254.169.254/latest/meta-data/"` → bị từ chối cứng `400 remote_media_url_disabled` (hoặc `403 ssrf_blocked_ip` nếu dự án bật tải URL ngoài).
- **`NEG-3` (`Cross-Tenant Idempotency Isolation`):** `Org A` và `Org B` tình cờ gửi trùng chuỗi `Idempotency-Key: req-123` → hoàn toàn độc lập, `0` rò rỉ phản hồi chéo tổ chức.

## 6. Test Commands & Required Evidence
- `CGO_ENABLED=0 TANPOPO_TEST_HERMETIC=1 go test -v -race ./internal/gateway/...`

## 7. Security Invariants & Rollback Plan
- **Security Invariant:** Vùng đệm `blob_ref` chỉ tồn tại trong vòng đời yêu cầu (xóa tức thì trong `defer`), không ghi tệp tạm không mã hóa xuống đĩa trừ khi vượt ngưỡng tràn cấu hình có mã hóa tạm thời, và không bao giờ chia sẻ khóa `Idempotency-Key` chéo `Virtual Key`.
- **Rollback:** Có thể tắt riêng từng điểm cuối mới qua cấu hình định tuyến HTTP mà không ảnh hưởng `/v1/chat/completions`.

## 8. Definition of Ready (DoR) & Definition of Done (DoD)
- **DoR:** `TP-IMP-0002` (khóa đặc tả `blob_ref` `CR-05`) và `TP-IMP-0015` đã hoàn tất.
- **DoD:** Xanh toàn bộ ca kiểm thử `CT-08`, `CT-09`, bài kiểm thử ảnh `20 MiB` qua Wasm `16 MiB` và vượt qua `REQUIRES_STRONG_REVIEW`.
