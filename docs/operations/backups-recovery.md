# Sao lưu Mã hóa, Lưu giữ/Xóa Dữ liệu, Phục hồi Thảm họa & Ma trận Bảo trì (Backups, Retention, Disaster Recovery & Support Matrix) — Tanpopo

> **Tài liệu Kỹ thuật SRE & Bảo toàn Dữ liệu (Phase ASTRA A06 — Updated in A08 Audit | `DRAFT / READY_FOR_REVIEW`)**
> - **Mã tài liệu:** `SPEC-OPS-02` | **Phiên bản:** `0.1.1-step08-audit` | **Ngày cập nhật:** `2026-11-14`
> - **Vai trò biên soạn:** Principal SRE & Security Architect
> - **Trạng thái quản trị:** **Đặc tả Vận hành Đề xuất (`PROPOSED — NO CODE`)** — gắn kết trực tiếp với lược đồ vật lý tại [`data-model.md`](../architecture/data-model.md) (`ARCH-DATA-01`) và topo [`deployment-topology.md`](../architecture/deployment-topology.md) (`ARCH-OPS-01`). **Mọi con số thời gian `RTO`, `RPO` và cửa sổ xóa `< 60 giây` trong tài liệu này đều mang nhãn `TARGET / UNVERIFIED` (chưa đo thực nghiệm).**
> - **Truy vết Yêu cầu:** `WF-12`, `WF-13`, `OPS-05`, `OPS-06`, `OPS-09`, `TP-FR-1003`, `TP-NFR-0501`, `TP-NFR-0502`, `TP-NFR-0601`, `TP-NFR-0602`, `TP-NFR-0701`, `TP-NFR-0901`, `TP-NFR-0902`.

---

## 1. Kiến trúc Sao lưu Tách Khóa Chủ (`Zero-Plaintext Backup Architecture` — `OPS-05`)

### 1.1. Quy tắc Vàng: Tách Biệt Tuyệt đối Giữa Bản Sao lưu Dữ liệu và `Master KEK`
Mọi thông tin xác thực nhạy cảm trong Tanpopo (`API Keys` nhà cung cấp, cặp `OAuth Tokens`, khóa ký Webhook) đều được mã hóa phong bì (`Envelope Encryption`: `AES-256-GCM` bằng khóa riêng từng tổ chức `Org DEK`, còn `Org DEK` được bọc bởi khóa bảo vệ chủ `Master KEK`). Do đó:
- **Bản sao lưu cơ sở dữ liệu KHÔNG BAO GIỜ chứa `Master KEK`:** Nếu tệp sao lưu lưu trên S3/NAS vô tình bị lộ, kẻ tấn công không thể giải mã bất kỳ khóa API nhà cung cấp nào.
- **Cảnh báo Mất Khả năng Khôi phục Khi Mất Khóa Chủ (`Master KEK Loss Hazard`):** Ngược lại, nếu quản trị viên làm mất cả máy chủ gốc lẫn `Master KEK` (và không dùng Cloud KMS có sao lưu tự động), các cấu hình tuyến, chính sách và sổ cái chi phí vẫn khôi phục được `100%`, nhưng toàn bộ `Provider Connections` sẽ phải nhập lại khóa mới. Vì vậy `SCR-01` và `SCR-16` luôn hiển thị `Banner` nhắc xác nhận lưu trữ `Master KEK` ở kho mật khẩu/Vault độc lập (`UX-GR-05`).

### 1.2. Phạm vi & Phương thức Sao lưu Theo Từng Chế độ Triển khai

| Thành phần Cần Sao lưu | Chế độ Đơn nút (`SQLite WAL`) | Chế độ Cụm HA (`PostgreSQL 16+` & `Valkey`) | Tần suất Khuyến nghị & Mục tiêu `RPO` |
|---|---|---|---|
| **1. Cơ sở dữ liệu Chính** (Cấu hình, Khóa bọc mã, Sổ cái `micro-USD`, Chuỗi `audit_events`) | Lệnh trực tuyến không khóa đọc/ghi: `tanpopo backup create --output backup.tpop.enc` (bên dưới gọi `VACUUM INTO` chụp ảnh nhất quán rồi nén `zstd` + ký mã băm `SHA-256`). | Sao lưu nền liên tục bằng **`pgBackRest` / `WAL-G`** (ảnh chụp đầy đủ hàng ngày + đẩy nhật ký `WAL` liên tục lên Object Storage để khôi phục tới từng thời điểm `PITR`). | Đơn nút: Mỗi `15 phút` – `1 giờ` (đẩy ra ngoài máy chủ).<br/>Cụm HA: Liên tục (`RPO < 5 phút`, đạt vượt mục tiêu `< 15 phút` — `TP-NFR-0601`). |
| **2. Tệp Khóa & Kho Gói Phần mở rộng** (`tanpopo.lock` + các tệp `.tpkg`/`.wasm` đã ghim) | Đóng gói tự động kèm trong tệp `backup.tpop.enc` (hoặc lưu trong kho GitOps của đội ngũ). | Đồng bộ trong kho lưu trữ đối tượng S3/OCI nội bộ có bật phiên bản (`Object Versioning`). | Sau mỗi thay đổi cài đặt/nâng cấp phần mở rộng. |
| **3. Tệp Cấu hình Khởi động Máy chủ** (`tanpopo.yaml` — không chứa khóa thô) | Quản lý trong Git (GitOps) + kèm bản chụp trong gói sao lưu. | Quản lý qua Helm `values.yaml` / Terraform trong Git. | Theo mỗi commit hạ tầng. |
| **4. Bộ nhớ đệm `Valkey / Redis`** | *(Không áp dụng — dùng RAM)* | **Không cần sao lưu bắt buộc** cho mục tiêu toàn vẹn: hạn mức tốc độ là ngắn hạn, còn sổ cái thật và danh sách khóa bị thu hồi luôn có nguồn gốc chuẩn trong PostgreSQL để tái tạo khi khởi động lại. | Tái tạo tự động từ PostgreSQL trong `< 10 giây`. |
| **5. Khóa Bảo vệ Chủ (`Master KEK`)** | Lưu ngoại tuyến độc lập trong trình quản lý bí mật của tổ chức (1Password, Vaultwarden, két sắt vật lý). | Lưu trữ và xoay vòng trong **AWS KMS / GCP KMS / Azure Key Vault / HashiCorp Vault** đa vùng. | Kiểm tra khả năng truy cập hàng quý. |

---

## 2. Chính sách Lưu giữ Phân tầng & Xóa Sạch Dữ liệu Dưới 60 Giây (`Retention & Crypto-Shredding`)

### 2.1. Bảng Thời hạn Lưu giữ Mặc định Bảo vệ Quyền riêng tư (`TP-NFR-0501`, `TP-BR-0802`)

| Phân loại Dữ liệu | Chế độ Mặc định Ngày đầu | Khoảng Cấu hình Cho phép | Cơ chế Dọn dẹp Tự động (Không Gây Khóa Bảng) |
|---|---|---|---|
| **Nội dung Văn bản Prompt / Phản hồi (`Debug Payloads`)** | **TẮT MẶC ĐỊNH (`0 byte` lưu đĩa)** | Nếu dự án chủ động bật để gỡ lỗi: **`TTL <= 72 giờ`** (trần cứng không cho phép đặt dài hơn). | Xóa cuốn chiếu theo lô nhỏ mỗi `10 phút` + bọc bằng khóa tạm ngắn hạn. |
| **Nhật ký Truy vết Siêu dữ liệu Kỹ thuật (`request_traces`)** | **BẬT** (`request_id`, độ trễ, mô hình, mã lỗi, số token, `accuracy`) | **`7 ngày` – `90 ngày`** (mặc định `30 ngày`). | Trên PostgreSQL: `DROP TABLE request_traces_YYYY_MM` tức thì theo phân vùng tháng; trên SQLite: xóa lô nhỏ `LIMIT 5000` ngoài giờ cao điểm. |
| **Bút toán Sổ cái Ngân sách (`usage_settlements`)** | **BẬT** (phục vụ đối soát tài chính & `Chargeback`) | **`>= 365 ngày`** (hoặc theo luật kế toán doanh nghiệp `1–7 năm`). | Lưu trữ phân vùng tháng chỉ đọc; hỗ trợ xuất sang kho lạnh Parquet/CSV trước khi cắt phân vùng cũ. |
| **Nhật ký Kiểm toán Bảo mật (`audit_events` nối chuỗi băm)** | **BẬT BẮT BUỘC (Bất biến — `TP-BR-0803`)** | **`>= 365 ngày`** (cấm xóa/sửa thủ công qua API hay Console). | Chỉ cho phép xuất niêm phong ký số ra kho lưu trữ bất biến (`S3 Object Lock WORM`) trước khi xoay vòng phân vùng quá hạn. |

### 2.2. Quy trình Xóa Vĩnh viễn Tổ chức / Dự án Trong `< 60 Giây` (`Tenant Crypto-Shredding` — `TP-NFR-0502`)
Khi `ORG_OWNER` xác nhận xóa một `Project` hoặc toàn bộ `Organization` (`WF-13`):
1. **Giây `0–5` (`Freeze & Stream Abort`):** Đánh dấu trạng thái `DELETING`, phát tín hiệu thu hồi toàn cụm (`< 5s`) để chặn mọi yêu cầu mới và đóng các luồng SSE đang chạy của đơn vị đó, quyết toán dứt điểm các khoản giữ chỗ đang treo.
2. **Giây `5–15` (`Cryptographic Key Destruction`):** Hủy vĩnh viễn khóa mã hóa dữ liệu riêng (`Tenant/Project DEK`) trong bảng khóa và gọi xóa vùng nhớ đệm trong `Blind Broker` trên mọi nút — khiến toàn bộ thông tin xác thực và dữ liệu nhạy cảm đã mã hóa của đơn vị đó (kể cả trên các bản sao lưu cũ chưa hết hạn) lập tức trở thành chuỗi nhiễu không thể khôi phục về mặt toán học.
3. **Giây `15–45` (`Relational Purge & Anonymized Receipt`):** Xóa toàn bộ bản ghi khóa ảo, kết nối, tuyến, chính sách và truy vết của đơn vị đó; chỉ ghi lại **1 bản ghi biên nhận kiểm toán ẩn danh duy nhất** (`tenant.crypto_shredded`, không chứa PII) để chứng minh tuân thủ quy định bảo vệ dữ liệu.

---

## 3. Bốn Kịch bản Diễn tập Phục hồi Thảm họa Từng Bước (`Disaster Recovery Runbooks` — `RTO < 15 Phút`)

Mọi kịch bản dưới đây đều tuân thủ nguyên tắc an toàn: **luôn khôi phục và kiểm chứng ở chế độ bảo trì nội bộ (`--maintenance-only` chỉ lắng nghe `127.0.0.1`) trước khi mở cổng nhận lưu lượng sản xuất**.

### `DR-01`: Mất Toàn bộ Máy chủ Đơn nút (`Standalone Host Total Loss`)
- **Mục tiêu:** Khôi phục máy chủ mới từ bản sao lưu `backup.tpop.enc` + `TANPOPO_MASTER_KEY` trong **`< 10 phút`**.
- **Các bước thực thi:**
  1. Cấp phát máy chủ mới, tải nhị phân `tanpopod` đúng phiên bản ghi trong siêu dữ liệu sao lưu.
  2. Nạp biến môi trường `TANPOPO_MASTER_KEY` từ kho bí mật của tổ chức.
  3. Chạy kiểm tra và bung bản sao lưu:
     ```bash
     tanpopo backup verify ./backup-latest.tpop.enc
     tanpopo backup restore ./backup-latest.tpop.enc --data-dir /var/lib/tanpopo --verify-secrets-decrypt
     ```
     *(Cờ `--verify-secrets-decrypt` thử mở phong bì `Org DEK` và kiểm tra chuỗi băm `audit_events` mà không in bất kỳ khóa nào ra màn hình).*
  4. Khởi động thử với `tanpopod serve --listen 127.0.0.1:8080`, chạy `tanpopo doctor` xác nhận `/readyz = 200` và toàn bộ gói trong `tanpopo.lock` đã nạp đủ, sau đó mở lại lưu lượng qua Reverse Proxy.

### `DR-02`: Hỏng Cơ sở Dữ liệu Cụm PostgreSQL Hoặc Thao tác Xóa Nhầm Hạ tầng (`Clustered DB PITR`)
- **Mục tiêu:** Khôi phục cụm HA về thời điểm ngay trước sự cố (`RPO < 5 phút`, `RTO < 15 phút`).
- **Các bước thực thi:**
  1. Tạm hạ số bản sao nhận tải hoặc bật chế độ bảo trì trên Ingress; dừng ghi từ `tanpopo-controlplane`.
  2. Khôi phục cụm PostgreSQL mới tới mốc thời gian mục tiêu (`pgbackrest --type=time "--target=2026-11-11 14:29:50+00" restore`).
  3. Chạy lệnh kiểm tra toàn vẹn sau khôi phục:
     ```bash
     tanpopo admin verify-state --check-audit-chain --reconcile-orphaned-holds --rebuild-valkey-state
     ```
     *(Lệnh này giải phóng các khoản giữ chỗ ngân sách `RESERVED_HOLD` bị treo dở đúng lúc sập và nạp lại toàn bộ danh sách khóa thu hồi vào Valkey trước khi mở cổng).*
  4. Khởi động lại các Pod `controlplane` và `dataplane`, xác nhận `/readyz = 200` rồi mở lại lưu lượng.

### `DR-03`: Nghi ngờ Rò rỉ Khóa Bảo vệ Chủ (`Master KEK Compromise & Envelope Re-Wrap`)
- **Mục tiêu:** Đổi sang khóa chủ `Master KEK` mới ngay lập tức mà không ngừng dịch vụ, sau đó xoay vòng các khóa API nhà cung cấp theo thứ tự ưu tiên.
- **Các bước thực thi:**
  1. Tạo khóa chủ thế hệ mới (`KEK_v2`) trong KMS/Vault, giữ khóa cũ (`KEK_v1`) ở chế độ chỉ cho giải mã tạm thời.
  2. Chạy lệnh bọc lại phong bì khóa (`Envelope Re-Wrap` — chỉ bọc lại vài chục khóa `Org DEK` nên hoàn tất trong `< 2 giây`, không cần giải mã/mã hóa lại hàng nghìn bản ghi):
     ```bash
     tanpopo vault rotate-kek --old-kek-id kek_v1 --new-kek-id kek_v2
     ```
  3. Vô hiệu hóa hoàn toàn `KEK_v1` trên KMS; nếu nghi ngờ kẻ tấn công đã kịp lấy được cả bản sao cơ sở dữ liệu lẫn `KEK_v1` trước đó, tiến hành xoay mới toàn bộ khóa API nhà cung cấp và thu hồi phiên OAuth tại `SCR-04`.

### `DR-04`: Nâng cấp Phiên bản Core / Phần mở rộng Gây Lỗi Nghiêm trọng (`Bad Release Rollback`)
- **Mục tiêu:** Quay lui toàn cụm về phiên bản ổn định liền trước trong **`< 60 giây`** (`SM-08`).
- **Các bước thực thi:**
  1. Nhờ kỷ luật chỉ chạy di trú lược đồ tương thích ngược (*Expand-Only*) ở bước nâng cấp, không cần khôi phục lạnh cơ sở dữ liệu.
  2. Nếu lỗi do một gói phần mở rộng vừa nâng cấp: chạy `tanpopo plugin rollback <slug>` (hoặc bấm trên `SCR-07`) → hoàn tất trong `< 5 giây`.
  3. Nếu lỗi do bản nhị phân Core mới: chạy `helm rollback tanpopo` (hoặc đổi lại thẻ ảnh container cũ trong `docker-compose.yml` và `up -d`) → các nút phiên bản cũ đọc lại lược đồ hiện tại và tệp ghim `tanpopo.lock` bình thường trong dưới `60 giây`.

---

## 4. Ma trận Hỗ trợ Nền tảng, Trình duyệt & Cam kết Tương thích Hợp đồng (`Maintenance & Support Matrix`)

| Hạng mục Hỗ trợ | Phạm vi Hỗ trợ Chính thức (`Tier-1 Supported`) | Hỗ trợ Nỗ lực Tốt nhất (`Best-Effort`) | Chính sách Ngưng Hỗ trợ & Vòng đời Bảo trì (`Deprecation Policy`) |
|---|---|---|---|
| **Hệ điều hành & CPU Máy chủ** (`TP-NFR-0901`) | Linux `x86_64` & `arm64` (kernel `>= 5.15`), Container OCI chuẩn, macOS `14+` (`arm64`), Windows Server `2022+` / Windows `11` (`x86_64`). | FreeBSD, kiến trúc `riscv64`, Windows `arm64`. | Theo chu kỳ hỗ trợ dài hạn (`LTS`) của hệ điều hành gốc. |
| **Cơ sở Dữ liệu & Bộ nhớ Đệm** | SQLite nhúng (tích hợp sẵn trong nhị phân), **PostgreSQL `16`, `17`**, **Valkey `7.2`, `8.x`** / Redis `7.2`. | Các dịch vụ tương thích giao thức PostgreSQL/Redis đám mây đã kiểm chứng Lua & `LISTEN/NOTIFY`. | Duy trì hỗ trợ tối thiểu 2 phiên bản chính gần nhất của PostgreSQL; thông báo trước `6 tháng` trước khi nâng mức tối thiểu. |
| **Trình duyệt Web Console** (`TP-NFR-0701`) | `2` phiên bản ổn định gần nhất của **Chromium / Chrome / Edge**, **Firefox** và **Safari** (macOS & iOS `>= 17`). | Các trình duyệt nhân Chromium doanh nghiệp khác. | Không hỗ trợ Internet Explorer hoặc trình duyệt tắt hoàn toàn ES2022 / CSS Cascade Layers (`@layer`). |
| **Hợp đồng Phần mở rộng & SDK** (`TP-NFR-0902`, `BA-OQ-005`) | Giao thức `tanpopo.extension.v1` và `Manifest Schema` phiên bản hiện hành. | Phiên bản liền trước trong cửa sổ chuyển tiếp. | - Giai đoạn `0.x`: Giữ tương thích tối thiểu **`N-1` phiên bản phụ** (`minor`).<br/>- Từ cột mốc `v1.0` trở đi: Cam kết bảo toàn tương thích ngược tối thiểu **`12 tháng` (hoặc trọn nhánh `1.x`)**, hiển thị cảnh báo sớm trên Console/CLI trước ít nhất `2` bản phát hành trước khi loại bỏ bất kỳ trường cũ nào. |
| **Bản vá An ninh (`Security Patches`)** | Nhánh phát hành ổn định hiện tại (`Current Minor`) và nhánh liền trước (`N-1 Minor` sau mốc `v1.0`). | Hướng dẫn giảm thiểu rủi ro qua `Policy` / `Quarantine` cho bản cũ hơn. | Lỗ hổng mức `Critical` (thoát khỏi Sandbox, lộ khóa gốc, vượt quyền chéo tổ chức) phát hành bản vá khẩn cấp và bản tin thu hồi trong mục tiêu `< 48 giờ` kể từ khi xác minh. |
