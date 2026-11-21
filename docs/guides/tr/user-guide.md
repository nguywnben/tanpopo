# Tanpopo Kullanıcı Kılavuzu

**Tanpopo**; deterministik yönlendirme, salt yazılır kimlik bilgisi güvenliği, kesin micro-USD FinOps muhasebesi ve çevrimdışı çalışma için geliştirilmiş, tek ikili dosyadan oluşan kendi barındırılan bir AI Ağ Geçidi ve Model Context Protocol (MCP) Merkezidir.

---

## 1. Hızlı Başlangıç ve İlk Kurulum

1. Tanpopo sunucusunu başlatın:
   ```bash
   tanpopo serve --role all-in-one --listen 127.0.0.1:8080
   ```
2. İlk başlatmada Tanpopo, sunucu günlüğüne tek kullanımlık bir **başlatma belirteci (bootstrap token)** yazdırır.
3. Tarayıcınızda `http://127.0.0.1:8080/setup` adresini açın ve 4 adımlı kurulum sihirbazını tamamlayın:
   - **Sahip hesabı:** Başlatma belirtecini, yönetici e-postasını, parolayı (en az 12 karakter), kuruluş adını ve ilk proje adını girin.
   - **İlk bağlantı (isteğe bağlı):** Ticari bir API sağlayıcısı (OpenAI, Anthropic, Google Gemini veya OpenAI uyumlu) ya da yerel bir motor (vLLM, Ollama) bağlayın. Yerel uç noktalar için özel ağ erişimini açıkça etkinleştirip joker olmayan CIDR'ler (örneğin `127.0.0.1/32`) belirtin.
   - **Anahtar saklama:** Örnek ana şifreleme anahtarını (KEK) yedeklediğinizi onaylayın. Ana anahtar kaybolursa şifreli kasa sırları kurtarılamaz.
   - **İnceleme ve başlatma:** Kurulumu tamamlayın, tek seferlik Sanal Anahtarınızı güvenle saklayın ve ilk akış isteğini doğrulayın.

---

## 2. Dil ve Görünüm

- **Web Konsolu:** Üst çubuktaki (veya Kurulum ve Giriş sayfalarındaki) dil seçiciyi kullanın. Varsayılan **Auto** seçeneği, tarayıcı tercihlerinizdeki ilk desteklenen dili seçer ve eşleşme yoksa English'e döner. Manuel seçimler sayfa yenilemelerinde korunur ve dilediğiniz zaman **Auto** seçeneğine dönebilirsiniz.
- **CLI:** `--lang <kod>` parametresini geçin (örneğin `tanpopo --lang tr --help`) veya `TANPOPO_LANG` / `LC_ALL` / `LC_MESSAGES` / `LANG` ortam değişkenlerini ayarlayın. Komut adları, bayraklar ve makine tarafından okunan çıktılar tüm dillerde sabit kalır.
- **API Hataları ve Uyarılar:** `/admin/v1/*` kontrol düzlemi uç noktaları, okunabilir `error.message` metinleri ve operasyonel uyarı özetleri için `Accept-Language` başlığını uygular; `error.code`, `error.type` ve JSON şemalarını sabit tutar.
- **Çevrimdışı Çalışma:** Tüm 15 dil, yazı tipleri ve simgeler ikili dosyaya gömülüdür—çalışma zamanında sıfır harici CDN isteği yapılır.

---

## 3. Temel Konsol İş Akışları

### Genel Bakış ve İzler
- **Genel Bakış:** Canlı istek sayısını, hata oranını, p95 proxy ek yükünü, kesinleşen micro-USD harcamasını, etkin blokeleri, devre kesicisi açık dağıtımları ve doğrudan düzeltme bağlantılı etkin uyarıları gösterir.
- **İzler:** Yalnızca meta veri içeren yürütme telemetrisini kaydeder (istem veya yanıt içeriği saklanmaz). Aşama bazlı zaman çizelgesini ve ölçüm doğruluğunu (`Kesin`, `Tahmini` veya `Ölçülmemiş`) incelemek için herhangi bir isteğe tıklayın.

### Sağlayıcılar, Modeller ve Yönlendirme
- **Sağlayıcılar:** API anahtarlarını, AWS IAM kimlik bilgilerini, webhook HMAC sırlarını veya TLS sertifikalarını salt yazılır kasada mühürleyin—düz metin bir kez gönderilir ve geri döndürülmez. Adaptörleri upstream uç noktalarına bağlayın ve canlı sınamalar çalıştırın.
- **Modeller ve Dağıtımlar:** Mantıksal modelleri yetenekleriyle bildirin ve negatif olmayan tamsayı **nano-USD** tarife kartlarıyla sağlayıcı bağlantılarına eşleştirin.
- **Rotalar ve Politikalar:** Deterministik yönlendirme (`Öncelikli yük devretme`, `Ağırlıklı`, `Ağırlıklı sıralı dağıtım`, `En düşük maliyet`, `En düşük gecikme (EWMA)`) yapılandırın. **Rota Açıklama** ile upstream çıkışı veya bütçe blokesi olmadan prova simülasyonu yapın. Kuruluş, Proje ve Ortam düzeyinde yalnızca sıkılaşabilen politikalar tanımlayın.

### Uzantılar, MCP Merkezi ve Deneme Alanı
- **Uzantılar:** Çevrimdışı önbellekli Mağaza'dan veya 64 karakterlik SHA-256 ile sabitlenmiş URI'den paket hazırlayın. Etkinleştirme öncesi izin farklarını inceleyin ve riskli paketleri hemen karantinaya alarak etkin akışları kesin.
- **MCP Merkezi:** Streamable HTTP MCP sunucularını kaydedin, araçları eşitleyin, RFC 8785 JCS şema özetlerini sabitleyin, risk sınıflarını (`read_only`, `mutating`, `destructive`) atayın ve belirli Sanal Anahtarlara araç izni verin.
- **Deneme Alanı:** Yalnızca geçerli sekmede kalan bütçe tavanlı geçici oturum anahtarıyla istemleri test edin; hazır cURL, Python ve TypeScript OpenAI SDK kod örneklerini kopyalayın.

### Erişim, FinOps, Güvenlik ve Operasyonlar
- **Erişim ve Anahtarlar:** Ortama bağlı Sanal Anahtarlar oluşturun, 24 saatlik geçiş penceresiyle döndürün ya da kademeli boşaltma veya acil kesinti ile iptal edin. 6 rollü RBAC (`Operatör`, `Kuruluş Sahibi`, `Ekip Yöneticisi`, `Güvenlik İnceleyicisi`, `FinOps Görüntüleyici`, `Geliştirici`) atamalarını yönetin.
- **FinOps:** Kesinleşen harcamaları, tahmini tutarları, etkin blokeleri ve ölçülmemiş istekleri izleyin; tamsayı micro-USD tavanları belirleyin ve CSV/JSON raporları dışa aktarın.
- **Güvenlik:** Yıkıcı MCP çağrıları için insan onaylarını (HITL) yönetin, 4 düzeyli acil Kill Switch'leri tetikleyin veya kaldırın ve kriptografik RFC 8785 JCS denetim zincirini doğrulayın.
- **Operasyonlar (`Operatör` rolü):** `/readyz` hazırlığını ve küme anlık görüntü dönemlerini izleyin, şifreli `.tpop.enc` yedekleri alın, yükseltme uyumluluğu ön kontrolleri çalıştırın ve HMAC imzalı webhook'lar kaydedin.

---

## 4. CLI Komut Özeti

| Komut | Amaç |
| --- | --- |
| `tanpopo serve` | Tanpopo sunucusunu başlatır (`--role all-in-one`, `control-plane`, `data-plane` veya `worker`). |
| `tanpopo doctor` | İkili dosyayı, veritabanı bağlantısını ve depolama izinlerini doğrular (`--json` destekli). |
| `tanpopo support bundle` | Hassas verilerden arındırılmış tanılama arşivi toplar (kasa düz metni veya istem içermez). |
| `tanpopo admin reset-owner` | Yerel kuruluş sahibinin parolasını çevrimdışı sıfırlar (`--yes-i-know-this-resets-owner-password` gerektirir). |
| `tanpopo backup create / restore` | Şifreli `.tpop.enc` yedek arşivi oluşturur veya geri yükler. |
| `tanpopo plugin install / upgrade / rollback / quarantine` | Özetle sabitlenmiş uzantıları komut satırından yönetir. |
| `tanpopo upgrade preflight` | Tanpopo Core yükseltmesi öncesi yüklü uzantıların ABI uyumluluğunu doğrular. |
| `tanpopo keys / routes / audit / kill-switch` | `/admin/v1` üzerinden uzak kontrol düzlemi kaynaklarını yönetir. |
