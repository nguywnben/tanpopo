# Đặc tả Vòng đời Phần mở rộng Đa Nguồn & Quản trị Marketplace (Multi-Source Lifecycle & Marketplace Governance) — Tanpopo

> **Tài liệu Đặc tả Kiến trúc & Quản trị Chuỗi Cung ứng (Phase ASTRA A04 — `DRAFT / ILLUSTRATIVE CANDIDATE SPEC`)**
> - **Mã tài liệu:** `SPEC-EXT-04` | **Phiên bản:** `0.1.0-step04-draft` | **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Platform Architect & Security-Minded SDK Designer
> - **Tuyên bố Trạng thái Chuẩn hóa:** Cấu trúc tệp khóa `tanpopo.lock`, chỉ mục Marketplace tĩnh và luồng ký số dưới đây **mang tính minh họa thiết kế (`ILLUSTRATIVE — PRE-ADR`)**, chưa tự nhận là chuẩn đóng băng trước khi chốt ADR chuỗi cung ứng.
> - **Truy vết Yêu cầu:** `DEC-002`, `DEC-004`, `SM-01`, `SM-08`, `WF-04`, `WF-11`, `WF-12`, `OPS-08`, `OPS-09`, `TP-FR-0304`..`0307`, `TP-FR-0703`, `TP-FR-1002`, `TP-FR-1004`, `TP-NFR-0403`.

---

## 1. Nguyên tắc An ninh Nền tảng: Phân biệt `Origin` ≠ `Publisher Trust` ≠ `Safety`

Một sai lầm nguy hiểm trong các hệ thống plugin là coi *"gói có chữ ký hợp lệ"* đồng nghĩa với *"an toàn tuyệt đối"*. Tanpopo tách bạch 3 khái niệm độc lập:

| Chiều đánh giá | Câu hỏi trả lời | Bằng chứng kỹ thuật | Giới hạn thực tế (**`Signature != Safety`**) |
|---|---|---|---|
| **1. `Package Origin`** *(Nguồn tải gói)* | *"Gói tạo tác này được tải về từ kênh vận chuyển nào?"* | Chỉ mục Marketplace chính thức, URL kho Git (gắn mã băm commit), kho OCI nội bộ hay tệp `.tpkg`/`.zip` cục bộ. | Kênh tải an toàn (HTTPS/OCI) chỉ chống sửa đổi trên đường truyền, không chứng minh tác giả là ai. |
| **2. `Publisher Trust`** *(Độ tin cậy nhà phát hành)* | *"Ai đã đóng gói phiên bản này và gói đó đã qua cấp kiểm định nào?"* | Chữ ký mật mã (Ed25519 / Sigstore OIDC) và nhãn phân cấp `official`, `verified` hoặc `unverified-external` (`TP-BR-0201`). | Chữ ký chỉ chứng minh **tính toàn vẹn và chống chối bỏ** kể từ lúc ký. Nếu khóa ký của nhà phát triển cộng đồng bị lộ hoặc mã nguồn có lỗ hổng logic, chữ ký vẫn hoàn toàn hợp lệ! |
| **3. `Runtime Safety`** *(An toàn hành vi thực tế)* | *"Gói này được phép chạm vào tài nguyên nào nếu nó bị lỗi hoặc bị chiếm quyền?"* | Bảng quyền đã duyệt (`Approved Permission Grant`), vùng cô lập Sandbox mặc định đóng, `Blind Credential Broker` và hạn mức CPU/RAM. | **Áp dụng bắt buộc cho MỌI gói (kể cả `official` và `verified`)** — không bao giờ tắt Sandbox chỉ vì gói có chữ ký số. |

---

## 2. Bốn Phương thức Cài đặt Chuẩn & Quy tắc Đóng gói Khép kín (`Hermetic Packaging`)

### 2.1. Bốn Nguồn Cài đặt Độc lập với Chu kỳ Phát hành Core

| Phương thức cài đặt | Cú pháp minh họa (`CLI` / Khai báo) | Cơ chế ghim bất biến & Phòng vệ | Cột mốc dự kiến |
|---|---|---|---|
| **1. Curated Marketplace** | `tanpopo plugin install @tanpopo/openai@^1.2.0` | Đối chiếu chỉ mục tĩnh đã ký số của dự án Tanpopo, kiểm tra chữ ký gói, mã băm SHA-256 và danh sách thu hồi (`Revocation Feed`). | `v0.2` (bộ cốt lõi đóng gói sẵn ở `v0.1`) |
| **2. Git URL / Release / Commit** | `tanpopo plugin install git+https://github.com/org/ext.git#v0.4.1` | Nếu người dùng truyền tên nhánh/tag, trình cài đặt bắt buộc phân giải thành **mã `commit SHA` đầy đủ** + mã băm gói tạo tác trong `tanpopo.lock`; **tuyệt đối không chạy bất kỳ script cài đặt tự động nào** (`postinstall`, `Makefile`) trên máy chủ Gateway. | `v0.1` (`Must`) |
| **3. Signed Package / Archive** | `tanpopo plugin install ./acme-adapter-0.2.0.tpkg` | Giới hạn kích thước giải nén tối đa (mặc định `<= 64 MB`), chặn đường dẫn tương đối thoát thư mục (`../` *Zip-Slip*), từ chối mọi liên kết tượng trưng (`symlinks`) trỏ ra ngoài và kiểm tra mã băm/chữ ký đính kèm. | `v0.1` (`Must`) |
| **4. OCI Artifact / Private Registry** | `tanpopo plugin install oci://registry.corp.internal/ai-ext/acme:0.2.0` | Tải theo chuẩn OCI Artifact; khóa cứng theo **`digest` (`@sha256:...`)** thay vì chỉ tin vào thẻ nhãn động (`:latest`), hỗ trợ xác thực kho riêng và đóng gói ngoại tuyến (`Air-gapped Bundle`). | `v0.2` (`Should`) / `v1.0` (`Air-gapped`) |

### 2.2. Quy tắc Phụ thuộc Không Rò rỉ (`Zero Transitive Runtime Dependency Rule`)
- **Đóng gói tự chứa tại thời điểm biên dịch (`Build-Time Bundling Only`):** Mọi thư viện phụ thuộc của một `Extension Package` phải được tác giả đóng gói sẵn vào tệp `.wasm` (Tầng 2) hoặc ảnh container cố định (Tầng 3) từ trước khi xuất bản. Máy chủ Tanpopo **cấm tuyệt đối** việc gọi `npm install`, `pip install` hay tải mã phụ thuộc phát sinh từ Internet lúc cài đặt phần mở rộng.
- **Không phụ thuộc chéo giữa các Plugin (`Flat Dependency Graph`):** Trong giai đoạn `v0.1`–`v1.0`, một `Extension` chỉ giao tiếp với `Plugin Contract` của Tanpopo Core, không gọi trực tiếp hàm nội bộ của một `Extension` khác nhằm loại bỏ hoàn toàn xung đột phiên bản hình thoi (*Diamond Dependency Problem*).

---

## 3. Vòng đời Vận hành 9 Chặng Từ Kiểm định Tới Cách ly (`SM-01` Deep Dive)

```mermaid
sequenceDiagram
    actor Op as Operator
    actor Rev as Reviewer / Org Owner
    participant Inst as Installer & Verifier
    participant Stage as Staging & Consent Gate
    participant Lock as tanpopo.lock & Audit
    participant RT as Isolated Runtime Pool

    Op->>Inst: 1. Yêu cầu cài đặt / cập nhật phiên bản mới
    Inst->>Inst: 2. Verification: Kiểm tra kích thước, chống Zip-Slip, SHA-256, chữ ký & Contract Version
    Inst->>Stage: 3. Staging: Bóc tách Manifest tĩnh (KHÔNG thực thi mã của gói ở bước này!)
    Stage->>Stage: 4. Permission Diff: So sánh quyền yêu cầu với bản đang chạy (nếu là Update)
    alt Cài mới HOẶC Bản cập nhật đòi thêm quyền (Permission Escalation)
        Stage-->>Rev: Trình Bảng Quyền & Nhãn Trust Tier (Giữ ở PENDING_APPROVAL / UPDATING_PENDING_RECONSENT)
        Rev->>Stage: Phê duyệt tường minh danh sách quyền
    end
    Stage->>RT: 5. Self-Test: Nạp vào Sandbox nháp & chạy kiểm tra khởi tạo cô lập (Dry-Run Health)
    RT-->>Lock: 6. Activation: Ghi mã băm + quyền đã duyệt vào tanpopo.lock & ghi AuditEvent
    Lock->>RT: Chuyển lưu lượng mới sang bản mới; rút êm luồng đang chạy trên bản cũ (DRAINING)
    opt Phát hiện lỗi vận hành -> Rollback
        Op->>RT: 7. `tanpopo plugin rollback` -> Chuyển tức thì về mã băm liền trước trong tanpopo.lock
    end
    opt Phát hiện mã độc / vượt quyền mạng / Advisory khẩn cấp
        Op->>RT: 8. `tanpopo plugin quarantine` -> Ngắt tức khắc thực thi & mạng, giữ nguyên dấu vết điều tra
    end
```

### Minh họa Cấu trúc Tệp Khóa Bất biến `tanpopo.lock` (`ILLUSTRATIVE — PRE-ADR`)

```yaml
# MINH HỌA TỆP KHÓA CHUỖI CUNG ỨNG (ILLUSTRATIVE CANDIDATE — Chờ chốt ADR)
lockfileVersion: 1
generatedAt: "2026-11-11T15:00:00Z"

extensions:
  "@acme-cloud/regional-llm-adapter":
    version: "0.2.0"
    origin: "oci://registry.corp.internal/ai-ext/acme@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
    artifactSha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    contractVersion: "0.1"
    trustTier: "unverified-external"
    orgInternalSignatureVerified: true
    approvedPermissions:
      approvedBy: "principal:usr_sec_lead_01"
      approvedAt: "2026-11-11T14:58:12Z"
      allowedHosts:
        - "inference.eu-central-1.acme-ai.example"
        - "inference.ap-northeast-1.acme-ai.example"
      secretBindingModes:
        primary_api_key: "host_blind_inject"
        hmac_signing_secret: "host_crypto_sign"
    previousPinnedRollback:
      version: "0.1.9"
      artifactSha256: "4b227777d4dd1fc61c6f884f48641d02b4d121d3fd328cb08b5531fcacdabf8a"
```

### Quy tắc Gỡ cài đặt (`Uninstall`) vs Cách ly (`Quarantine`)
- **`Uninstall` (Gỡ sạch có điều kiện):** Chỉ thực hiện được khi `0` `Provider Connection` và `0` `Policy` đang tham chiếu tới phần mở rộng đó; xóa tệp tạo tác khỏi bộ nhớ đệm thực thi nhưng vẫn bảo toàn các dòng `AuditEvent` lịch sử.
- **`Quarantine` (Cách ly khẩn cấp — `TP-BR-0404`):** Thực thi ngay lập tức bất kể đang có bao nhiêu kết nối tham chiếu; mọi yêu cầu đi tới kết nối phụ thuộc bị từ chối an toàn với mã `503 extension_quarantined`, gói tạo tác bị khóa niêm phong để phục vụ điều tra pháp y số và tra cứu bán kính ảnh hưởng (`TP-FR-0705`).

---

## 4. Quản trị Marketplace, Xuất xứ Phần mềm (`SBOM / Provenance`) & Thu hồi (`Revocation`)

1. **Xác minh Danh tính Nhà phát hành (`Publisher Verification`):**
   - Không gian tên `@tanpopo/*` được khóa độc quyền bằng khóa ký gốc ngoại tuyến của dự án Tanpopo.
   - Các nhà phát hành đăng ký nhãn `verified` phải chứng minh quyền sở hữu tên miền tổ chức (DNS TXT) hoặc định danh kho mã nguồn qua chữ ký OIDC minh bạch (hướng chuẩn Sigstore/Cosign), để chống tấn công đặt tên gần giống (*typosquatting*).
2. **Quy trình Kiểm duyệt (`Moderation Pipeline`):**
   - *Bước tự động (CI Gate):* Kiểm tra lược đồ `Manifest`, quét giấy phép, kiểm tra đính kèm **SBOM** (`CycloneDX` hoặc `SPDX`), chạy toàn bộ bộ kiểm thử băng ghi `tanpopo plugin test` (`CT-01`..`CT-18`) và từ chối tự động các gói xin quyền mạng toàn cục `*`.
   - *Bước thẩm định con người (`Marketplace Reviewer`):* Rà soát tuân thủ điều khoản nhà cung cấp — từ chối dứt khoát mọi gói trích xuất phiên thuê bao tiêu dùng cá nhân hoặc giả mạo thương hiệu (`TP-BR-0302`, `TP-AC-SEC-06`).
3. **Giấy phép, SBOM & Chứng thực Xuất xứ (`License, SBOM & Provenance`):**
   - Mọi gói trên Official Marketplace bắt buộc khai báo giấy phép chuẩn SPDX (chỉ nhận giấy phép nguồn mở tương thích hoặc giấy phép phân phối rõ ràng, cảnh báo nếu chứa thành phần copyleft hạn chế), đính kèm danh mục thành phần `sbom.cdx.json` và bản chứng thực nguồn biên dịch (`Build Provenance`) trỏ về đúng commit mã nguồn.
4. **Bản tin An ninh & Danh sách Cảnh báo Thu hồi (`Security Advisories & Revocation Feed`):**
   - Dự án duy trì tệp dữ liệu cảnh báo tĩnh được ký số (`revocations.json`), phân loại mức độ:
     - `CRITICAL_MALICIOUS` (phát hiện mã độc / đánh cắp dữ liệu): Máy chủ tự vận hành có bật đồng bộ cảnh báo sẽ tự động chuyển gói khớp mã băm sang `QUARANTINED` (hoặc bắn cảnh báo khẩn cấp nếu chạy chế độ chỉ cảnh báo).
     - `VULNERABLE_UPGRADE_ADVISED` (lỗi bảo mật đã có bản vá): Hiển thị cảnh báo đỏ trên Web Console và đầu ra của lệnh `tanpopo doctor`.
   - Đối với môi trường mạng kín (`Air-gapped`), `Operator` có thể nhập tệp `revocations.json` ngoại tuyến qua CLI.
5. **Kênh Báo cáo Vi phạm & Lỗ hổng (`Reporting & Takedown`):**
   - Công bố quy trình công bố lỗ hổng có trách nhiệm (`Coordinated Vulnerability Disclosure`) và biểu mẫu báo cáo vi phạm bản quyền / vi phạm điều khoản nhà cung cấp với cam kết tạm ẩn khẩn cấp gói độc hại trên chỉ mục công khai ngay khi xác minh bằng chứng.
