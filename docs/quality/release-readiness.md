# Cổng Chất lượng Phát hành, Kiểm soát Hạ Chuẩn & Gói Trình Duyệt Gate G3 (Release Readiness & Gate G3 Package) — Tanpopo

> **Tài liệu Quản trị Chất lượng Xuất xưởng & Tổng kết Gate G3 (Phase ASTRA A07 — Gate G3: `READY_FOR_REVIEW` | `PROPOSED — NO CODE`)**
> - **Mã tài liệu:** `SPEC-QA-04` | **Phiên bản:** `0.1.0-step07-draft` | **Ngày cập nhật:** `2026-11-12`
> - **Vai trò biên soạn:** Principal Security Engineer, QA Architect & Release Engineering Lead
> - **Trạng thái quản trị:** **Cổng Gate G3 tiếp tục giữ nguyên trạng thái `READY_FOR_REVIEW` (`CHƯA APPROVED`)**. Áp dụng kỷ luật chống tự ý hạ chuẩn từ [`constraint-driven-development`](../../.agents/skills/constraint-driven-development/SKILL.md) và [`definition-of-done.md`](../../.agents/references/definition-of-done.md).
> - **Truy vết Yêu cầu:** `DEC-001`..`008`, `SD-G1-01`..`05`, `TD-G2-01`..`06`, `ADR-0001`..`0006`, `RSK-001`..`016`, `SPEC-SEC-01`..`03`, `SPEC-QA-01`..`03`.

---

## 1. Chính sách Phân loại Mức độ Nghiêm trọng Của Lỗi (`Defect Severity Policy`)

Mọi lỗi phát hiện trong kiểm thử nội bộ, quét bảo mật hoặc báo cáo cộng đồng được phân vào **4 mức nghiêm trọng** với quy tắc chặn phát hành không có ngoại lệ ngầm:

| Mức Độ | Định nghĩa Kỹ thuật & Tác động | Quy tắc Chặn Phát hành (`Release Gate Rule`) | Cam kết Phản ứng & Yêu cầu Bắt buộc Khi Đóng Lỗi |
|---|---|---|---|
| **`Sev-0 / P0`**<br/>*(Thảm họa / Khẩn cấp An ninh)* | Thoát khỏi vùng cô lập Wasm/Sidecar; rò rỉ khóa xác thực gốc hoặc dữ liệu chéo tổ chức; `SSRF` đọc được metadata đám mây; mất/sai lệch tiền sổ cái không gắn cờ; sập nhân `tanpopod`; vi phạm ranh giới pháp lý `DEC-004`. | **CHẶN TUYỆT ĐỐI MỌI BẢN PHÁT HÀNH (`0` lỗi `P0` mở).** Nếu phát hiện trên bản đã phát hành: phát hành bản vá khẩn + cập nhật `revocations.json` ngay lập tức. | Khắc phục khẩn cấp; bắt buộc viết bài kiểm thử tái hiện lỗi đỏ trước khi sửa (*Prove-It Regression Test*) và rà soát nguyên nhân gốc `5 Whys`. |
| **`Sev-1 / P1`**<br/>*(Nghiêm trọng / Gãy Tính năng Cốt lõi)* | Gãy luồng suy luận chính, chuyển mạch dự phòng sai quy tắc (như fallback sau mảnh đầu tiên), retry nhầm công cụ có tác động phụ, hỏng nâng cấp/quay lui `N-1`, hoặc lỗi tiếp cận mức `critical` chặn thao tác bàn phím trên luồng chính. | **CHẶN PHÁT HÀNH (`0` lỗi `P1` mở).** Chỉ có thể lùi tính năng mới sang bản sau (tắt hẳn sau cờ), không được phát hành tính năng đang mang lỗi `P1`. | Phải có bài kiểm thử tự động khóa chặt hành vi đúng trong `Stage 2 / Stage 3 CI` trước khi đóng ticket. |
| **`Sev-2 / P2`**<br/>*(Trung bình / Có Đường Đi Vòng)* | Lỗi hiển thị hoặc hành vi biên trên tính năng phụ không ảnh hưởng an ninh, toàn vẹn tài chính hay độ sẵn sàng, và có cách xử lý tạm thời rõ ràng. | Tối đa `<= 3` lỗi `P2` đã ghi nhận công khai trong mục `Known Issues` của Ghi chú Phát hành kèm kế hoạch bản sửa lỗi kế tiếp. | Lên lịch xử lý trong nhịp phát triển liền kề. |
| **`Sev-3 / P3`**<br/>*(Nhỏ / Thẩm mỹ & Câu chữ)* | Chi tiết căn chỉnh nhỏ, câu thông báo chưa mượt mà, gợi ý cải tiến công thái học CLI. | Không chặn phát hành. | Gom xử lý theo lô cải tiến chất lượng. |

---

## 2. Chốt Chặn Chống Tự Ý Hạ Chuẩn Chất lượng (`Constraint Floor Guard — CONSTRAINTS.md Blueprint`)

Để ngăn hiện tượng người lập trình hoặc AI Agent âm thầm tắt cảnh báo để "ép xanh CI", ngay khi bắt đầu giai đoạn khởi tạo kho mã (sau khi Owner duyệt), dự án thiết lập tệp `CONSTRAINTS.md` và kịch bản `Stage 0 Floor Guard` tự động đánh trượt PR nếu xuất hiện bất kỳ dấu hiệu nào sau đây mà không có phê duyệt tường minh của `Security / QA Lead`:
1. **Cấm Bỏ qua Hoặc Xóa Kiểm thử Ngầm:** Thêm `t.Skip()`, `.skip`, `.only`, `xit` không gắn mã ticket đang mở, hoặc xóa bớt các ca kiểm thử `CT-*` / `AUTH-*` / `FZ-*` hiện có.
2. **Cấm Tắt Công cụ Phân tích Tĩnh & Bảo mật:** Thêm chỉ thị bịt miệng trình kiểm tra như `//nolint:gosec`, `/* eslint-disable */`, `@ts-ignore`, `@ts-expect-error`, `nosemgrep`, `# gitleaks:allow`, hoặc khối `catch {}` rỗng nuốt lỗi.
3. **Cấm Hạ Ngưỡng Kiểm định Trong Cấu hình:** Giảm ngưỡng độ phủ kiểm thử của các gói trọng yếu (`vault`, `sandbox`, `policy`, `ledger`, `mcp` — mục tiêu độ phủ dòng `>= 85%` `TARGET / UNVERIFIED`), nới lỏng ngân sách độ trễ hay hạ cấp quy tắc `astryx doctor`.
4. **Cấm Vi phạm Quy ước Astryx UI:** Đưa thẻ `div` / `span` thô, mã màu `#hex`/`px` cứng, tệp `.css` tự viết hoặc lệnh `theme eject` vào phần giao diện Web Console (`AGENTS.md`).

---

## 3. Bảy Cổng Kiểm định Sẵn sàng Xuất xưởng (`Release Quality Gates — GATE-REL-01 .. 07`)

Một bản phát hành (`v0.1`, `v0.2`, `v0.3`, `v1.0`) chỉ được phép gắn thẻ Git và xuất bản khi vượt qua đủ **7 cổng bằng chứng đo được**:

| Mã Cổng | Hạng mục Kiểm định | Bằng chứng Bắt buộc Để Vượt Cổng (`Required Evidence`) | Chủ thể Ký Duyệt (`Human Signoff`) |
|---|---|---|---|
| **`GATE-REL-01`**<br/>**Bằng chứng Kiểm thử & Khép kín Lỗi** | Toàn bộ tầng kiểm thử & độ phủ | `100%` xanh trên `Stage 0..4 CI`; `0` lỗi `Sev-0`/`Sev-1`; kịch bản `EXT-ACCEPT-01` đạt trọn 10 bước không đổi mã băm nhị phân Core; `0` rò rỉ chuỗi `Canary Secret`. | `QA Architect` |
| **`GATE-REL-02`**<br/>**Ma trận Tương thích Đa Nền tảng & Hợp đồng** | Hệ điều hành, DB, Trình duyệt & `N-1` | Xác minh nhị phân trên `Linux (amd64/arm64)`, `macOS (arm64)` và `Windows (amd64)`; xanh đồng thời trên cả `SQLite WAL` và `PostgreSQL 16+`; chạy êm gói phần mở rộng chuẩn `N-1`; kiểm tra trình duyệt Chrome/Edge/Firefox/Safari hiện hành. | `Platform / QA Lead` |
| **`GATE-REL-03`**<br/>**Bảo mật, Fuzzing & Phạm vi Kiểm thử Xâm nhập (`Pen-Test Scope`)** | An ninh ứng dụng, Sandbox & Đa thuê bao | - **Mốc `v0.1` – `v0.3`:** Vượt qua `100%` ma trận đối kháng nội bộ (`CT-03`..`05`, `CT-14`..`23`, `CT-25`..`31`, `AUTH-01`..`16`), chạy fuzzing `FZ-01`..`06` tối thiểu `4 giờ` không lỗi, `0` lỗ hổng `Critical/High` có đường chạm tới trên `govulncheck`/`osv-scanner`.<br/>- **Trước mốc `v1.0` (`GA`):** Bắt buộc hoàn thành **đánh giá kiểm thử xâm nhập hộp xám (`Grey-Box Penetration Test`)** bao trùm 5 mặt trận: (1) Thoát `wazero` Wasm Sandbox & cạn kiệt tài nguyên, (2) Rút ruột khóa qua `Blind Broker` & kênh kề, (3) Vượt `SSRFGuard` / `DNS Rebinding`, (4) Vượt `PostgreSQL RLS`, `AEAD AAD` & `Cache` chéo tổ chức, (5) Giả mạo `OAuth 2.1` / `MCP Rug-Pull`. | `Security Lead` |
| **`GATE-REL-04`**<br/>**Bản quyền Mở `Apache-2.0` & Chuỗi Cung ứng** | Giấy phép, `SBOM`, Chữ ký số & Tuân thủ `DEC-004` | Đã đồng bộ [`package.json`](../../package.json) về `"license": "Apache-2.0"` (xử lý dứt điểm `ISS-002`); công cụ quét giấy phép xác nhận **`0` thư viện phụ thuộc mang giấy phép copyleft lây nhiễm (`GPL` / `AGPL` / `SSPL`)** trong nhị phân phân phối; đính kèm đầy đủ `sbom.cdx.json`, chứng thực `SLSA` và chữ ký `Cosign`; `100%` tuân thủ `DEC-004`. | `Product Owner` + `Security Lead` |
| **`GATE-REL-05`**<br/>**Chất lượng Tài liệu, Hợp đồng API & Giao diện** | Đồng bộ tài liệu – code & Khả năng tiếp cận | Đặc tả `OpenAPI 3.1` và tài liệu lệnh `tanpopo --help` khớp `100%` với mã thực tế (kiểm tra tự động chống lệch tài liệu); `npx @astryxdesign/cli doctor --built` đạt `0 fail`; quét `axe-core` đạt `WCAG 2.2 AA`. | `DX / UI Lead` |
| **`GATE-REL-06`**<br/>**Sẵn sàng Vận hành Thực chiến (`SRE Ops Readiness`)** | Khởi chạy, Giám sát, Sao lưu & Sổ tay sự cố | Lệnh `tanpopo doctor` kiểm tra đủ 8 hạng mục; đã bấm giờ diễn tập thành công 4 kịch bản khôi phục `DR-01`..`04` (`RTO < 15m` `TARGET / UNVERIFIED`) và gói cài đặt mạng kín `Air-Gap`; mọi cảnh báo `PAGE` đều gắn liên kết hợp lệ tới `INC-01`..`07`. | `SRE / Ops Lead` |
| **`GATE-REL-07`**<br/>**Quy trình Leo thang & Phê duyệt Cuối Của Con người** | Quản trị phát hành | **AI Agent không bao giờ có quyền tự ký duyệt qua cổng.** Mọi đề xuất xin miễn trừ tạm thời (chỉ áp dụng cho `Sev-2`, tuyệt đối không áp dụng cho `Sev-0/Sev-1` hay vi phạm bảo mật) phải lập biên bản đánh giá rủi ro bằng văn bản và được **Product Owner** phê chuẩn trực tiếp. | **`Product Owner`** *(Quyền quyết định tối cao)* |

---

## 4. Gói Tổng hợp Trình Duyệt Cổng Gate G3 (`Consolidated Gate G3 Review Package`)

> **Trạng thái hiện tại của Gate G3 (cùng Gate G1 & Gate G2):** **`READY_FOR_REVIEW`** *(Tuyệt đối không tự đánh dấu `APPROVED` khi Product Owner chưa phản hồi phê duyệt).*

### 4.1. Danh mục Các Quyết định Chặn Cần Product Owner Chốt Trước Khi Bước Vào Thực thi (`Blocking Decisions`)

Để chuyển từ giai đoạn Thiết kế & Đặc tả (`A00` → `A07`) sang giai đoạn Lập kế hoạch công việc chi tiết / Chạy PoC / Viết mã, Product Owner cần xem xét phê duyệt (hoặc điều chỉnh) **4 nhóm quyết định đang mở** tại [`decisions.md`](../governance/decisions.md):

| Nhóm Cổng | Các Mã Quyết định Đang Chờ Duyệt | Phương án Đề xuất Nhất quán Xuyên suốt `A01`–`A07` (`PROPOSED`) | Lý do Chặn Không Thể Viết Code Trước Khi Chốt |
|---|---|---|---|
| **1. Chiến lược Sản phẩm (`Gate G1`)** | `SD-G1-01` .. `SD-G1-05` | Nhắm mũi nhọn vào `Dev Teams` & `AI Platform Teams` tự vận hành; thực thi phần mở rộng đa tầng + `Blind Broker`; phân kỳ 3 nhịp (`v0.1` Core+Plugin → `v0.2` MCP → `v0.3` A2A+HA); kiểm duyệt chặt kênh chính thức theo `DEC-004`; thuần giấy phép **Apache-2.0** (sửa `ISS-002`). | Quyết định phạm vi tính năng `v0.1`, giấy phép tệp gốc và mô hình tin cậy hệ sinh thái. |
| **2. Phạm vi & Nghiệp vụ (`Gate G2` & `A02`)** | `TD-G2-01` .. `TD-G2-06`<br/>`BA-OQ-001` .. `BA-OQ-005` | Đặt `MCP` ở `v0.2`; hỗ trợ cả `Unified API` + `Native Passthrough`; giữ chỗ ngân sách trước luồng + kết chuyển âm nhẹ cuối luồng; tách thu hồi êm vs thu hồi khẩn cấp cắt luồng; duyệt `destructive` kép (sync `<= 60s` + async token); cam kết tương thích hợp đồng `N-1` (`0.x`) / `12 tháng` (`1.x`). | Quyết định trực tiếp lược đồ cơ sở dữ liệu, máy trạng thái vòng đời và hợp đồng API. |
| **3. Kiến trúc Nền tảng (`Gate G3 — ADRs`)** | [**`ADR-0001`**](../architecture/adr/0001-core-runtime-and-modular-monolith.md) .. [**`ADR-0006`**](../architecture/adr/0006-immutable-config-snapshots-and-fast-revocation.md) | **Go `1.24+` (`CGO_ENABLED=0`) Modular Monolith**; Sandbox 3 tầng dùng **`wazero`**; Lưu trữ kép **`SQLite WAL` + `PostgreSQL 16+ / Valkey`**; giao diện SPA tĩnh **`Astryx v0.7.13 (neutral)`** dời vào `web/console/` nhúng vào binary Go; Sổ cái số nguyên `BIGINT micro-USD`; và `Copy-on-Write ConfigSnapshot` thu hồi `< 1s`/`< 5s`. | Đây là các quyết định một chiều (*One-Way Doors*): không thể khởi tạo `go.mod`, dời thư mục `src/` hay viết lược đồ SQL khi chưa chốt `ADR-0001`..`0004`. |
| **4. Chính sách Mở Theo Từng Hãng (`A04`)** | `POL-01`, `POL-02` | Chỉ mở `Authorized OAuth 2.1` trên `Official Adapter` cho hãng có tài liệu cấp phép bên thứ ba công khai; giữ mô-đun ủy quyền MCP linh hoạt cho nhịp `v0.2`. | Bảo vệ an toàn pháp lý và tránh khóa cứng vào dự thảo đặc tả bên ngoài chưa ổn định. |

---

### 4.2. Ánh xạ 1-1 Giữa Rủi ro Trọng yếu (`P0 / High`) Và Bài Kiểm thử Nghiệm thu Cấp `P0` (`P0 Risk Acceptance Tests`)

Mỗi rủi ro mức cao trong [`risk-register.md`](../discovery/risk-register.md) đều đã được khóa bằng ít nhất một **Bài Kiểm thử Nghiệm thu `P0` có tiêu chí Đạt/Trượt nhị phân**:

| Mã Rủi ro | Tên Rủi ro Trọng yếu | Biện pháp Kiểm soát Cốt lõi (`SPEC-SEC / ARCH`) | Bài Kiểm thử Nghiệm thu `P0` Bắt buộc (`Pass/Fail Gate`) |
|---|---|---|---|
| **`RSK-001`** | Lộ khóa xác thực gốc hoặc thoát khỏi Sandbox qua Plugin bên thứ ba | `Tier-1 Declarative` + `Tier-2 Wazero (0 FS/Env/Net)` + `Blind Host Credential Broker` + `AEAD AAD` | **`CT-14`**, **`CT-16`**, **`CT-17`**, **`FZ-05`** & **`OPS-AC-10`** *(Quét `0 byte` khóa rõ trong RAM khách Wasm, nhật ký và luồng lỗi).* |
| **`RSK-002`** | Ôm đồm phạm vi làm trễ ngày ra mắt bản khả dụng | Phân kỳ cuốn chiếu `v0.1 -> v0.2 -> v0.3` (`SD-G1-03`) + giữ nhân Core bất biến | **`EXT-ACCEPT-01`** *(Chứng minh nhân `v0.1` tinh gọn nạp được Adapter ngoài trọn vòng đời mà không sửa Core).* |
| **`RSK-003`** | Ô nhiễm pháp lý / điều khoản dịch vụ bởi công cụ lách thuê bao cá nhân | Cưỡng chế `DEC-004` ở cả DB (`billing_mode`), cổng Marketplace, nhãn nguồn gốc và UI (`UX-GR-03`) | **`CT-20`** & **`AUTH-16`** *(Đánh trượt tự động mọi gói trích xuất cookie / `client_id` thuê bao cá nhân và chặn mạo danh `official`).* |
| **`RSK-004`** | Độ trễ cao hoặc rò rỉ bộ nhớ ở biên truyền luồng `Host <-> Wasm` | Ưu tiên Tầng 1 gốc Go cho ~75–80% hãng + hồ tái sử dụng phiên `wazero` giới hạn trang nhớ | **`SPIKE-02`** & **`CT-07`** *(Chứng minh `p95 < 5 ms` overhead và `< 1 ms/chunk` dưới `1.000` luồng SSE — hiện là `TARGET / UNVERIFIED`).* |
| **`RSK-005`** | Bẫy "Con gà – Quả trứng" của Marketplace & sự mục nát (`Bit-Rot`) của Plugin cộng đồng | Đóng gói sẵn bộ `Official Reference Adapters` ngày đầu + kiểm thử băng ghi tự động định kỳ | **`CT-01`..`CT-13`** & **`SPIKE-01`** *(Tạo gói khai báo `< 30 phút` và chạy kiểm thử băng ghi `tanpopo plugin test` tự động phát hiện gãy hợp đồng).* |
| **`RSK-006`** | Đầu độc công cụ MCP (*Rug-Pull*), tiêm nhiễm chỉ thị & retry tác động phụ | Lọc ACL theo khóa ảo + ghim băm `schema_hash` JCS + `0` retry trên `mutating`/`destructive` + duyệt người thật khớp băm tham số | **`CT-21`**, **`CT-22`** & **`FZ-04`** *(Đổi `1` ký tự mô tả công cụ lập tức khóa `403`; quá hạn mạng không bao giờ gọi lặp lần 2).* |
| **`RSK-007`** | Vượt trần ngân sách do đua tranh song song hoặc ghi nhận `0 USD` khi đứt luồng | Giữ chỗ hai pha `Pre-Request Hold` trên số nguyên `micro-USD` + phân loại `EXACT / ESTIMATED / UNMETERED_FLAGGED` | **`CT-10`**, **`CT-12`**, **`CT-29`** & **`FZ-06`** *(`50` luồng đua tranh có `0` vượt trần; `10.000` bước ngẫu nhiên bảo toàn tuyệt đối tổng tiền).* |
| **`RSK-008`** | Đóng băng sớm Hợp đồng Plugin thiếu sót gây gãy vỡ hệ sinh thái | Tự dùng `Plugin Contract` cho `Official Adapters` (*dogfooding*) + bảo hành tương thích lùi `N-1` / `12 tháng` | **`CT-24`** & **`OPS-AC-05`** *(Gói `N-1` chạy không sửa đổi; `upgrade preflight` chặn nâng cấp nếu có gói `N-2` hết hạn hỗ trợ).* |
| **`RSK-009`** | Rơi rớt tính năng tiên tiến của mô hình mới do chuẩn hóa mẫu số chung thấp nhất | Hỗ trợ song song `Unified API` + `Lossless Native Dialect Passthrough` + kiểm tra năng lực trả `400` rõ ràng | **`CT-08`** & **`CT-13`** *(Giữ nguyên `100%` trường JSON mở rộng trên `/native/*`; chặn `400` thay vì cắt tham số ngầm).* |
| **`RSK-010`** | Quá tải đội ngũ duy trì mã nguồn mở & chi phí nuôi hạ tầng chợ | Giữ nhân Core gọn, đẩy tích hợp ra Extension + chỉ mục Marketplace tĩnh trên Git/OCI + công cụ tự chẩn đoán | **`EXT-ACCEPT-01`** & **`GATE-REL-05`** *(Cài mới Adapter không phát hành lại Core; `tanpopo doctor` + `support-bundle` tự chẩn đoán).* |
| **`RSK-011`** | Độ trễ thu hồi khóa hoặc nút mù thông tin trên cụm phân tán | Truyền tin 3 lớp (`Valkey` + `PG LISTEN/NOTIFY` + nhịp `2s`) + ngắt mạch nút mù `> 10s` (`FMEA-07`) | **`AUTH-06`** & **`OPS-AC-03`** *(Cắt luồng đang mở `< 1s`/`< 5s`; nút mất kết nối nguồn chân lý quá `10s` tự chuyển `/readyz = 503`).* |
| **`RSK-012`** | Xung đột giấy phép (`ISS-002`) hoặc kéo nhầm thư viện copyleft (`GPL`/`AGPL`) | Đồng bộ `Apache-2.0`, nhị phân Go thuần `CGO_ENABLED=0` + quét giấy phép/`SBOM` ở `Stage 0 CI` | **`GATE-REL-04`** *(`0` thư viện `GPL`/`AGPL`/`SSPL` trong nhị phân phân phối; khớp `Apache-2.0` + đính kèm `sbom.cdx.json`).* |
| **`RSK-013`** | Lệch hành vi giữa `SQLite WAL` và `PostgreSQL 16+` | 6 quy tắc kiểu dữ liệu khả chuyển sau `storage/port` + kiểm tra đối soát di trú | **`Stage 2 Dual-DB CI`** & **`CT-32`** *(Mọi bài tích hợp phải xanh đồng thời trên cả 2 DB; di trú khớp `100%` tổng sổ cái và bản mã).* |
| **`RSK-014`** | Tấn công `SSRF` & `DNS Rebinding (TTL=0)` đọc siêu dữ liệu đám mây | Kiểm tra địa chỉ IP đã phân giải ngay trong móc `net.Dialer.Control` lúc `connect()` + lột tiêu đề khi chuyển hướng | **`CT-15`** & **`CT-26`** *(`0` gói tin TCP SYN chạm vào `169.254.169.254` hoặc `127.0.0.1` kể cả khi DNS nhảy IP ở `TTL=0`).* |
| **`RSK-015`** | Nổ chuỗi thời gian `Telemetry Cardinality` hoặc nghẽn ngược bộ thu `OTLP` | Khóa cứng tập nhãn cố định + vòng đệm OTLP `32 MiB` xả cũ nhất (`Drop-Oldest` — `Fail-Open`) | **`OPS-AC-12`** & **`TP-AC-F0804`** *(Bắn `100.000` `request_id` ngẫu nhiên không làm tăng số dòng `/metrics`; tắt bộ thu OTLP không chặn luồng suy luận).* |
| **`RSK-016`** | Rò rỉ câu trả lời nhạy cảm chéo tổ chức qua bộ nhớ đệm phản hồi | Mặc định tắt cache; khóa cache gắn cứng `org_id + project_id + env_id + policy_hash + tool_acl_hash` mã hóa bằng `Org DEK` (`FMEA-03`) | **`CT-28`** & **`AUTH-14`..`15`** *(`0` lần trúng cache chéo tổ chức/môi trường/chính sách; `FORCE RLS` + `AEAD AAD` chặn mọi truy cập chéo).* |

---

### 4.3. Phạm vi Chứng minh Cụ thể Của Các PoC (`SPIKE-01` .. `SPIKE-04`) & Đề nghị Xin Phép Product Owner

Trước khi đóng băng cứng chuẩn giao diện nhị phân Wasm (`POC-01`..`03`) và viết mã sản phẩm quy mô lớn, kiến trúc đề xuất **4 bài thử nghiệm kỹ thuật nhỏ, cô lập, có giới hạn thời gian rõ ràng (`Timeboxed Spikes`)**. Theo đúng kỷ luật quản trị, bảng dưới đây làm rõ **mỗi PoC chứng minh điều gì, KHÔNG chứng minh điều gì, và chỉ được khởi chạy khi Product Owner cho phép**:

| Mã PoC | Thời lượng Tối đa | Điều PoC Này Sẽ Chứng minh Thực nghiệm (`What It Proves`) | Giới hạn — Điều PoC Này KHÔNG Chứng minh (`What It Does NOT Prove`) | Hệ quả Quyết định Sau Khi Đo |
|---|---|---|---|---|
| **`SPIKE-01`**<br/>*Độ phủ Ngôn ngữ Gói Khai báo Tầng 1* | `1 ngày` | Tập toán tử JSON/template không Turing-complete có đủ sức biểu diễn trọn vẹn giao thức của `>= 8/10` nhà cung cấp AI thực tế (gồm streaming SSE, lỗi chuẩn và trích xuất 5 loại token) trong `< 30 phút/gói` hay không (`POC-02`, `ASM-004`). | Chưa chứng minh khả năng chịu tải hàng nghìn kết nối hay bảo mật của Tầng 2 Wasm. | Khóa lược đồ `Declarative Provider Schema v0.1` tối giản nhất. |
| **`SPIKE-02`**<br/>*Hiệu năng & Cô lập Của `wazero` Wasm Streaming + `Blind Broker`* | `2 ngày` | Đo số thực tế trên Go `1.24` + `wazero`: độ trễ cộng thêm `p95`/`p99` dưới `1.000` luồng SSE, khả năng cắt ngang vòng lặp vô tận `while(1)`, trần RAM phiên Wasm và xác nhận `0 byte` khóa thật nằm trong bộ nhớ tuyến tính Wasm (`POC-01`, `ASM-002`, `ASM-005`, `RSK-001`, `RSK-004`). | Không đại diện cho sản phẩm hoàn chỉnh (chưa có DB, UI hay quản trị vòng đời). | Khóa chuẩn `Host-Guest Streaming ABI` (chọn giữa Linear-Memory Ring Buffer vs WIT Preview 2) dựa trên số đo thật. |
| **`SPIKE-03`**<br/>*Đua tranh Giữ chỗ Ngân sách Hai Pha (`SQLite WAL` vs `PG + Valkey`)* | `1 ngày` | Tốc độ và tính nguyên tử của thuật toán giữ chỗ `Pre-Hold` + quyết toán `micro-USD` dưới `500` luồng song song trên cả `SQLite WAL` đơn nút lẫn `Valkey Lua + PostgreSQL` (`ASM-006`, `RSK-007`, `RSK-013`). | Chưa bao gồm giao diện báo cáo `FinOps` hay tích hợp thanh toán ngoài. | Xác nhận thông lượng ghi đơn nút SQLite và chốt ngưỡng cần chuyển sang cụm PostgreSQL. |
| **`SPIKE-04`**<br/>*Làm mới Đơn luồng `Authorized OAuth 2.1` & Chốt chặn `MCP schema_hash`* | `1 ngày` | Kiểm chứng khóa `Single-Flight Refresh` chống bão làm mới token dưới `100` yêu cầu đồng thời và tốc độ tính toán dấu vân tay chuẩn hóa `RFC 8785 JCS` cho danh mục `300` công cụ MCP (`POL-01`, `POL-02`, `RSK-006`). *(Lưu ý: `POC-03` về IPC cho `Tier-3 Sidecar` phụ thuộc quyết định phạm vi `CR-02`).* | Không chứng minh chính sách thương mại của từng hãng AI ngoài đời (`POL-01` vẫn phải xét theo điều khoản từng hãng). | Khóa thiết kế mô-đun `OAuth Broker` và bộ lọc `MCP Tool Firewall`. |

> **Xác nhận Ranh giới Thực thi & Xin Phép Product Owner:**
> - Tính đến thời điểm hoàn tất bước **ASTRA A07**, **chưa có bất kỳ dòng mã PoC, mã kiểm thử thực thi hay mã ứng dụng frontend/backend nào được viết vào repository**.
> - Sau khi xem xét gói tài liệu Gate G3 này, kính đề nghị **Product Owner xác nhận phê duyệt các quyết định mở (`Gate G1 / G2 / G3`) và cho phép tường minh** trước khi đội ngũ bắt đầu viết mã cho `SPIKE-01`..`04` hoặc lập kế hoạch triển khai mã nguồn sản phẩm ở giai đoạn tiếp theo.