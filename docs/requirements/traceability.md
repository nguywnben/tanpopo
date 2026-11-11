# Ma trận Truy vết Đầu-Cuối & Tự Kiểm tra Cổng G2 (End-to-End Traceability Matrix) — Tanpopo

> **Tài liệu Kiểm chứng Truy vết (Phase ASTRA A03 — Gate G2: `READY_FOR_REVIEW` | `DRAFT — UNLOCKED SCOPE`)**
> - **Phiên bản:** `0.1.0-step03-draft`
> - **Ngày cập nhật:** `2026-11-11`
> - **Vai trò biên soạn:** Principal Requirements Engineer & Product Manager
> - **Trạng thái quản trị:** **Bản nháp chưa khóa phạm vi (`DRAFT`)**. Thiết lập đường truy vết hai chiều không đứt gãy từ Khám phá (`UC`) & Phân tích Nghiệp vụ (`WF`, `OPS`, `SM`, `TP-BR`) sang Định nghĩa Sản phẩm (`FT`, `TP-FR`, `TP-NFR`), Tiêu chí Nghiệm thu (`TP-AC`) và **khung giữ chỗ (`Placeholders`)** cho Thiết kế Kiến trúc, Đơn vị Mã nguồn & Bộ Kiểm thử ở các bước tiếp theo (**A04+**).

---

## 1. Quy ước Khung Giữ chỗ Hạ nguồn (`Downstream Placeholders` cho Pha A04+)

Do kiến trúc kỹ thuật và cấu trúc mã nguồn chi tiết chưa được khóa ở bước A03 (`OQ-001`..`OQ-007`), các cột hạ nguồn sử dụng mã định danh giữ chỗ chuẩn hóa để bước Thiết kế Kiến trúc (**A04**) và Lập trình/Kiểm thử điền trực tiếp mà không làm gãy liên kết:

- **`ARCH-TBD-01` .. `ARCH-TBD-10`:** Tài liệu thiết kế kiến trúc / ADR / đặc tả giao diện cho từng phân hệ trong `Capability Map` (`prd.md` Mục 5).
- **`MOD-TBD-<module>`:** Đơn vị gói mã nguồn thực thi tương ứng của từng phân hệ (ví dụ `MOD-TBD-gateway-protocols`, `MOD-TBD-plugin-marketplace`...).
- **`TST-TBD-<suite>`:** Bộ kiểm thử tự động tương ứng (`TST-TBD-unit`, `TST-TBD-contract`, `TST-TBD-integration`, `TST-TBD-security`, `TST-TBD-perf`, `TST-TBD-ui-a11y`).

---

## 2. Ma trận Truy vết Chiều Thuận: `Use Case / Workflow → Business Rule → Feature → FR/NFR → AC → Design/Unit/Test`

| Use Case / Hành trình (`UC` / `WF` / `OPS` / `CAND`) | Quy tắc Nghiệp vụ & Máy trạng thái (`TP-BR` / `SM`) | Tính năng (`FT`) | Yêu cầu Hệ thống (`TP-FR` / `TP-NFR` & Ưu tiên) | Tiêu chí Nghiệm thu (`TP-AC`) | Giữ chỗ Thiết kế (`ARCH`) | Giữ chỗ Đơn vị Code (`MOD`) | Giữ chỗ Kiểm thử (`TST`) |
|---|---|---|---|---|---|---|---|
| **`UC-01`**, **`WF-06`**, `CAND-011`<br/>*(Unified Inference, Multimodal, Partial Stream, Cancel)* | `TP-BR-0101`, `0103`, `0503`, `0504`, `0505`<br/>`SM-04`, `SM-07` | `FT-01` | `TP-FR-0101`..`0104` (`Must`)<br/>`TP-FR-0106` (`Later`)<br/>`TP-NFR-0101`, `0102` (`Must`) | `TP-AC-F0101`..`F0104`<br/>`TP-AC-N01` | `ARCH-TBD-08` *(Ingress & Streaming Spec)* | `MOD-TBD-gateway-protocols` | `TST-TBD-integration`<br/>`TST-TBD-perf` *(Placeholder Post-v1 cho `0106`)* |
| **`UC-01`**, **`WF-06`**, `CAND-011`<br/>*(Lossless Native Dialect Passthrough)* | `TP-BR-0101`, `0304`, `0501`, `0503`, `0702`<br/>`SM-04`, `SM-07` | `FT-02` | `TP-FR-0105` (`Should`)<br/>`TP-NFR-0102` (`Must`) | `TP-AC-F0105`<br/>`TP-AC-SEC-04` | `ARCH-TBD-08` *(Native Passthrough Spec)* | `MOD-TBD-gateway-protocols` | `TST-TBD-contract`<br/>`TST-TBD-security` |
| **`UC-02`**, **`WF-03`**, `CAND-010`<br/>*(4-Entity Chain & Self-Hosted Local GPU Models)* | `TP-BR-0102`, `0104`, `0501`<br/>`SM-02`, `SM-04` | `FT-03` | `TP-FR-0201`..`0203` (`Must`) | `TP-AC-F0201`, `F0202`<br/>`TP-AC-SEC-01` | `ARCH-TBD-06` *(Provider & Deployment Model)* | `MOD-TBD-provider-credentials` | `TST-TBD-unit`<br/>`TST-TBD-integration` |
| **`UC-04`**, **`WF-03`**, **`WF-05`**, `CAND-005`, `CAND-006`<br/>*(Write-Only Vault, Authorized OAuth 2.1, KMS/IAM)* | `TP-BR-0102`, `0301`..`0304`, `0803`<br/>`SM-02`, `SM-03` | `FT-04` | `TP-FR-0204` (`Must`)<br/>`TP-FR-0205` (`Should`)<br/>`TP-FR-0206` (`Could`)<br/>`TP-NFR-0402` (`Must`) | `TP-AC-F0203`, `F0204`<br/>`TP-AC-SEC-01`, `SEC-03`, `SEC-06` | `ARCH-TBD-06` *(Vault & OAuth Broker Spec)* | `MOD-TBD-provider-credentials` | `TST-TBD-security`<br/>`TST-TBD-integration` |
| **`UC-03`**, **`WF-04`**, **`WF-06`**, `CAND-008`<br/>*(2-Tier Plugin Engine, Default-Deny Sandbox & Blind Broker)* | `TP-BR-0401`, `0402`, `0404`<br/>`SM-01` | `FT-05` | `TP-FR-0301`..`0303` (`Must`)<br/>`TP-NFR-0101`, `0202`, `0401` (`Must`) | `TP-AC-F0301`<br/>`TP-AC-SEC-01`, `SEC-02`<br/>`TP-AC-N01`, `N02` | `ARCH-TBD-05` *(Plugin Sandbox & Host ABI)* | `MOD-TBD-plugin-marketplace` | `TST-TBD-security`<br/>`TST-TBD-contract`<br/>`TST-TBD-perf` |
| **`UC-03`**, **`UC-09`**, **`WF-04`**, **`WF-11`**, `OPS-08`, `OPS-09`, `CAND-007`, `CAND-009`<br/>*(Installer, Provenance, SDK/Test, Escalation Gate, Rollback, Registry)* | `TP-BR-0201`..`0203`, `0302`, `0403`, `0801`<br/>`SM-01`, `SM-08` | `FT-06` | `TP-FR-0304`..`0306` (`Must`)<br/>`TP-FR-0307` (`Should`)<br/>`TP-NFR-0403`, `0902` (`Must`) | `TP-AC-F0302`..`F0304`<br/>`TP-AC-SEC-02`, `SEC-06`<br/>`TP-AC-N10` | `ARCH-TBD-05` *(Registry, Lockfile & SDK Spec)* | `MOD-TBD-plugin-marketplace` | `TST-TBD-contract`<br/>`TST-TBD-security` |
| **`UC-06`**, **`WF-09`**, `CAND-014`, `CAND-015`<br/>*(MCP Plane Scoping, `schema_hash`, Destructive Approval & Retry Block)* | `TP-BR-0104`, `0601`..`0603`, `0803`<br/>`SM-06` | `FT-07` | `TP-FR-0401`..`0404` (`Should`) | `TP-AC-F0401`..`F0403`<br/>`TP-AC-SEC-05`, `SEC-08` | `ARCH-TBD-09` *(MCP Governance Plane Spec)* | `MOD-TBD-mcp-a2a` | `TST-TBD-integration`<br/>`TST-TBD-security` |
| `OPS-02`, `CAND-016`<br/>*(A2A AgentCard Discovery, Async Task & Privilege Ceiling)* | `TP-BR-0104`, `0501`, `0604`<br/>`SM-06`, `SM-07` | `FT-08` | `TP-FR-0405`, `0406` (`Could`) | `TP-AC-F0404`<br/>`TP-AC-SEC-05`, `SEC-08` | `ARCH-TBD-09` *(A2A Delegation Plane Spec)* | `MOD-TBD-mcp-a2a` | `TST-TBD-integration`<br/>`TST-TBD-security` |
| **`UC-07`**, **`WF-02`**, **`WF-06`**, **`WF-10`**, **`WF-12`**, `OPS-02`, `CAND-002`..`004`<br/>*(Tenancy, RBAC, Virtual Key Rotation/Revoke, OIDC & Four-Eyes)* | `TP-BR-0101`..`0104`, `0803`<br/>`SM-05`, `SM-06` | `FT-09` | `TP-FR-0501`..`0503` (`Must`)<br/>`TP-FR-0504` (`Could`)<br/>`TP-NFR-0402`, `0404` (`Must`) | `TP-AC-F0501`..`F0504`<br/>`TP-AC-SEC-07`<br/>`TP-AC-N04` | `ARCH-TBD-02` *(IAM, Tenancy & Key Auth Spec)* | `MOD-TBD-iam-tenancy` | `TST-TBD-security`<br/>`TST-TBD-unit` |
| **`UC-05`**, **`WF-07`**, `OPS-07`, `CAND-012`<br/>*(Capability Router, Data Residency, Pre-Stream Failover & Dry-Run Explain)* | `TP-BR-0501`..`0505`<br/>`SM-04` | `FT-10` | `TP-FR-0601`..`0603` (`Must`)<br/>`TP-FR-0604` (`Should`) | `TP-AC-F0601`..`F0603`<br/>`TP-AC-SEC-05` | `ARCH-TBD-07` *(Router & Circuit Breaker Spec)* | `MOD-TBD-routing-resilience` | `TST-TBD-unit`<br/>`TST-TBD-integration` |
| **`UC-05`**, **`UC-07`**, **`WF-04`**, **`WF-07`**, `CAND-007`, `CAND-012`<br/>*(Hierarchical Policy, Fail-Closed, Rate Limit, SSRF & Subscription Compliance)* | `TP-BR-0202`, `0301`, `0302`, `0501`, `0502`, `0601`..`0604`<br/>`SM-06` | `FT-11` | `TP-FR-0701`..`0703` (`Must`)<br/>`TP-NFR-0402` (`Must`) | `TP-AC-SEC-04`..`SEC-07` | `ARCH-TBD-03` *(Policy Engine & Network Guard)* | `MOD-TBD-policy-security` | `TST-TBD-security`<br/>`TST-TBD-unit` |
| **`UC-07`**, **`UC-09`**, **`WF-12`**, `OPS-03`, `OPS-04`, `CAND-008`, `CAND-017`<br/>*(Immutable Audit Log & 4-Level Emergency Kill-Switches)* | `TP-BR-0404`, `0803`<br/>`SM-01`, `SM-02`, `SM-05` | `FT-12` | `TP-FR-0704`, `0705` (`Must`)<br/>`TP-NFR-0404`, `0501` (`Must`) | `TP-AC-F0701`<br/>`TP-AC-SEC-02`<br/>`TP-AC-N04` | `ARCH-TBD-03` *(Audit Sink & Kill-Switch Bus)* | `MOD-TBD-policy-security` | `TST-TBD-security`<br/>`TST-TBD-integration` |
| **`UC-08`**, **`WF-08`**, `OPS-09`, `CAND-013`<br/>*(Two-Phase Budget Hold, Accuracy-Tagged Settlement & Chargeback)* | `TP-BR-0701`..`0703`<br/>`SM-07` | `FT-13` | `TP-FR-0801`, `0802` (`Must`)<br/>`TP-FR-0803` (`Should`)<br/>`TP-FR-0804` (`Later`) | `TP-AC-F0801`..`F0803` | `ARCH-TBD-04` *(Two-Phase Budget Ledger Spec)* | `MOD-TBD-telemetry-finops` | `TST-TBD-integration`<br/>`TST-TBD-perf` *(Placeholder Post-v1 cho `0804`)* |
| **`WF-06`**, **`WF-13`**, `OPS-06`, `CAND-017`<br/>*(OpenTelemetry Export & Metadata-Only Privacy Telemetry)* | `TP-BR-0101`, `0502`, `0802` | `FT-14` | `TP-FR-0805`, `0806` (`Must`)<br/>`TP-NFR-0501`, `0502` (`Must`) | `TP-AC-F0804`<br/>`TP-AC-SEC-01`<br/>`TP-AC-N05` | `ARCH-TBD-04` *(OTLP & Redaction Pipeline)* | `MOD-TBD-telemetry-finops` | `TST-TBD-security`<br/>`TST-TBD-integration` |
| **`WF-02`**, **`WF-04`**, **`WF-09`**, **`WF-11`**, `OPS-01`, `OPS-07`, `CAND-001`<br/>*(Admin API `409`, Astryx Web Console `neutral` & `tanpopo` CLI)* | `TP-BR-0201`, `0203`, `0801`<br/>`SM-01`..`08` | `FT-15` | `TP-FR-0901`..`0903` (`Must`)<br/>`TP-FR-0904` (`Should`)<br/>`TP-NFR-0701`, `0702`, `0801`, `1001`, `1002` (`Must`) | `TP-AC-F0901`, `F0902`<br/>`TP-AC-F0303`, `F0403`, `F0603`<br/>`TP-AC-N07`, `N08`, `N10` | `ARCH-TBD-10` *(Admin API, CLI & Astryx UI Spec)* | `MOD-TBD-console-cli-api` | `TST-TBD-ui-a11y`<br/>`TST-TBD-integration` |
| **`WF-01`**, **`WF-11`**, **`WF-13`**, `OPS-01`, `OPS-05`, `OPS-06`, `OPS-09`, `CAND-001`, `CAND-017`<br/>*(Standalone Self-Host, GitOps Lockfile, Backup, Erasure, Preflight & HA)* | `TP-BR-0203`, `0802`, `0803`<br/>`SM-08` | `FT-16` | `TP-FR-1001`..`1004` (`Must`)<br/>`TP-FR-1005` (`Could`)<br/>`TP-NFR-0103`, `0201`, `0301`, `0302`, `0601`, `0602`, `0901`, `0902` | `TP-AC-F1001`..`F1003`<br/>`TP-AC-N01`..`N03`, `N05`, `N06`, `N09`, `N10` | `ARCH-TBD-01` *(Bootstrap, Storage & HA Topology)* | `MOD-TBD-selfhost-ops` | `TST-TBD-integration`<br/>`TST-TBD-perf` |

---

## 3. Thống kê Độ phủ Kiểm chứng Hai chiều (`Bidirectional Coverage Audit`)

| Chiều kiểm tra | Tổng số phần tử | Số phần tử đã ánh xạ đầy đủ | Phần tử mồ côi (`Orphans`) | Kết luận |
|---|---|---|---|---|
| **Use Cases Khám phá (`UC-01` .. `UC-09`)** | `9` | `9 / 9` (`100%`) | `0` | Đạt tuyệt đối |
| **Hành trình Nghiệp vụ (`WF-01` .. `WF-13`)** | `13` | `13 / 13` (`100%`) | `0` | Đạt tuyệt đối |
| **Yêu cầu Vận hành (`OPS-01` .. `OPS-09`) & Ứng viên (`CAND-001` .. `017`)** | `9` + `17` | `26 / 26` (`100%`) | `0` | Đạt tuyệt đối |
| **Máy Trạng thái (`SM-01` .. `SM-08`)** | `8` | `8 / 8` (`100%`) | `0` | Đạt tuyệt đối |
| **Quy tắc Nghiệp vụ (`TP-BR-0101` .. `TP-BR-0803`)** | `30` | `30 / 30` (`100%`) | `0` | Đạt tuyệt đối |
| **Yêu cầu Chức năng `Must` (`P0`) trong `srs.md`** | `36` | `36 / 36` (`100%` có `TP-AC-*`) | `0` | **Không có P0 thiếu AC** |
| **Yêu cầu Chức năng `Should` & `Could` trong `srs.md`** | `15` (`10 Should` + `5 Could`) | `15 / 15` (`100%` có `TP-AC-*`) | `0` | Đạt tuyệt đối |
| **Yêu cầu Chức năng `Later` (`Post-v1.0`: `TP-FR-0106`, `0804`)** | `2` | `2 / 2` (Đã gắn nhãn `Later` & Placeholder) | `0` | Tách biệt rõ khỏi `v0.1`–`v1.0` |
| **Yêu cầu Phi Chức năng (`TP-NFR-0101` .. `TP-NFR-1002`)** | `22` | `22 / 22` (`100%` có `TP-AC-N*` / `SEC-*`) | `0` | Đạt tuyệt đối |

---

## 4. Bảng Tự Kiểm tra Bắt buộc Cổng Gate G2 (`Mandatory Gate G2 Self-Check`)

| Tiêu chí tự kiểm tra bắt buộc của Gate G2 (Prompt ASTRA A03) | Kết quả | Bằng chứng kiểm chứng cụ thể trong bộ tài liệu |
|---|---|---|
| **1. Không có tính năng `P0` (`Must`) nào thiếu Tiêu chí Nghiệm thu (`AC`)?** | **ĐẠT (`PASS`)** | Toàn bộ `36/36` yêu cầu chức năng `Must` (`TP-FR-*`) và toàn bộ yêu cầu phi chức năng `Must` (`TP-NFR-*`) tại [`srs.md`](./srs.md) đều ánh xạ 1-1 hoặc n-1 tới các kịch bản `Given/When/Then` cụ thể trong [`acceptance-criteria.md`](./acceptance-criteria.md) (`TP-AC-F*`, `TP-AC-SEC-*`, `TP-AC-N*`). |
| **2. Không có tuyên bố "hỗ trợ đầy đủ" chung chung mà thiếu Ma trận Giao thức / Năng lực (`Protocol/Capability Matrix`)?** | **ĐẠT (`PASS`)** | Mục 1 của [`srs.md`](./srs.md) thiết lập bảng 14 dòng phân định rạch ròi `UNIFIED_TRANSLATED`, `NATIVE_PASSTHROUGH`, `ADAPTER_DECLARED_CAPABILITY`, `DEDICATED_PLANE`, `LATER_POST_V1` và `PROHIBITED_BY_POLICY`; từng mục `FT-01`..`FT-16` trong [`feature-catalog.md`](../product/feature-catalog.md) đều chỉ rõ hành vi nhà cung cấp hỗ trợ và không hỗ trợ. |
| **3. Không có kịch bản giá/mức tiêu thụ mô hình không xác định nào lại hứa hẹn tính đúng tuyệt đối `100%`?** | **ĐẠT (`PASS`)** | `TP-BR-0703`, `TP-FR-0802`, `TP-FR-0803` và `TP-AC-F0803` bắt buộc phân loại mọi giao dịch thành `EXACT`, `ESTIMATED` hoặc `UNMETERED_FLAGGED`, tách cột riêng trên báo cáo `Chargeback` và nghiêm cấm ghi `0 USD` giả tạo hoặc tuyên bố chính xác 100% khi luồng đứt giữa chừng/thiếu bảng giá. |
| **4. Không có tuyên bố hỗ trợ OAuth cho thuê bao tiêu dùng phổ quát (`Universal Consumer Subscription OAuth`)?** | **ĐẠT (`PASS`)** | `NG-03` & `PC-03` trong [`prd.md`](../product/prd.md), `FT-04` trong [`feature-catalog.md`](../product/feature-catalog.md), Mục 1 & `TP-FR-0205`/`0703` trong [`srs.md`](./srs.md), cùng `TP-AC-SEC-06` khẳng định rõ: SDK chỉ hỗ trợ `Authorized OAuth 2.1` được nhà cung cấp cấp phép hợp lệ, tuyệt đối không bao hàm quyền biến thuê bao cá nhân (`ChatGPT Plus/Pro`, `Codex`, `Claude Pro/Max`, `Antigravity`...) thành cổng API chung và cấm dùng hộp thoại "chấp nhận rủi ro" để lách điều khoản. |
| **5. Trạng thái bản nháp & thẩm quyền phê duyệt Cổng G2 được giữ đúng kỷ luật?** | **ĐẠT (`PASS`)** | Mọi tài liệu A03 đều đóng dấu **`DRAFT — UNLOCKED SCOPE`** (do Owner chưa duyệt Gate G1); **Gate G2** được đặt ở **`READY_FOR_REVIEW`** tại [`decisions.md`](../governance/decisions.md), hoàn toàn không tự ý nâng lên `APPROVED`. |
