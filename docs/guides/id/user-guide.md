# Panduan Pengguna Tanpopo

**Tanpopo** adalah AI Gateway dan Hub Model Context Protocol (MCP) self-hosted dalam satu biner, dirancang untuk perutean deterministik, keamanan kredensial hanya-tulis, akuntansi FinOps mikro-USD yang presisi, serta operasi siap offline.

---

## 1. Mulai Cepat & Penyiapan Awal

1. Jalankan server Tanpopo:
   ```bash
   tanpopo serve --role all --listen 127.0.0.1:8080
   ```
2. Pada peluncuran pertama, Tanpopo mencetak **token bootstrap** sekali pakai di log server.
3. Buka `http://127.0.0.1:8080/setup` di browser dan selesaikan panduan 4 langkah:
   - **Akun pemilik:** Masukkan token bootstrap, email admin, kata sandi (minimal 12 karakter), nama organisasi, dan nama proyek pertama.
   - **Koneksi pertama (opsional):** Hubungkan penyedia komersial (OpenAI, Anthropic, Google Gemini, atau kompatibel OpenAI) atau mesin lokal (vLLM, Ollama). Endpoint lokal wajib mengaktifkan akses jaringan privat dan mencantumkan CIDR eksak non-wildcard (misalnya `127.0.0.1/32`).
   - **Penjagaan kunci:** Konfirmasi bahwa Anda telah mencadangkan kunci enkripsi master (Master KEK). Kehilangan kunci ini membuat rahasia brankas tidak dapat dipulihkan.
   - **Tinjau & inisialisasi:** Selesaikan penyiapan, simpan Kunci Virtual sekali lihat dengan aman, dan verifikasi permintaan streaming pertama.

---

## 2. Bahasa & Tampilan

- **Konsol Web:** Gunakan pemilih bahasa di bilah atas (serta di halaman Penyiapan dan Masuk). **Auto** terpilih secara bawaan dan memilih bahasa didukung pertama dari preferensi browser (cadangan: English). Pilihan manual tetap tersimpan saat halaman dimuat ulang dan dapat dikembalikan ke **Auto** kapan saja.
- **CLI:** Gunakan `--lang <kode>` (misalnya `tanpopo --lang id --help`) atau variabel `TANPOPO_LANG`, `LC_ALL`, `LC_MESSAGES`, `LANG`. Nama perintah, opsi, dan keluaran terstruktur tetap konsisten.
- **Error API & Peringatan:** Endpoint `/admin/v1/*` mengikuti header `Accept-Language` untuk `error.message` dan ringkasan peringatan, sambil menjaga `error.code`, `error.type`, dan skema JSON tetap stabil.
- **Operasi Offline:** Seluruh 15 bahasa, font, dan ikon dibundel dalam biner—nol permintaan CDN eksternal saat berjalan.

---

## 3. Alur Kerja Utama Konsol

### Ringkasan & Jejak
- **Ringkasan** menampilkan permintaan langsung, tingkat error, overhead proxy p95, biaya mikro-USD terselesaikan, tahan aktif, deployment bersirkuit terbuka, dan peringatan aktif dengan tautan penanganan langsung.
- **Jejak** hanya menyimpan telemetri metadata (tanpa prompt atau jawaban). Klik permintaan apa pun untuk melihat waterfall tahap eksekusi dan akurasi (`Eksak`, `Estimasi`, atau `Tidak terukur`).

### Penyedia, Model & Perutean
- **Penyedia:** Segel kunci API, kredensial AWS IAM, rahasia HMAC webhook, atau sertifikat TLS di brankas hanya-tulis. Ikat adaptor ke endpoint upstream dan jalankan uji koneksi langsung.
- **Model & Deployment:** Deklarasikan model logis dengan kapabilitasnya lalu ikat ke koneksi penyedia dengan kartu tarif integer **nano-USD**.
- **Rute & Kebijakan:** Atur perutean deterministik (`Failover prioritas`, `Berbobot`, `Round robin berbobot`, `Biaya terendah`, `Latensi terendah (EWMA)`). Gunakan **Analisis Rute** dalam mode dry-run tanpa trafik keluar. Kebijakan Organisasi, Proyek, dan Lingkungan hanya boleh memperketat batas induknya.

### Ekstensi, Pusat MCP & Playground
- **Ekstensi:** Siapkan paket dari Marketplace offline atau lewat URI + SHA-256 64 karakter. Tinjau perbedaan izin sebelum mengaktifkan dan karantina paket bermasalah seketika.
- **Pusat MCP:** Daftarkan server MCP Streamable HTTP, sinkronkan alat, sematkan hash skema RFC 8785 JCS, tetapkan risiko (`read_only`, `mutating`, `destructive`), dan izinkan ke Kunci Virtual tertentu.
- **Playground:** Uji prompt dengan kunci sesi sementara berbatas anggaran serta salin cuplikan cURL, Python, dan TypeScript.

### Akses, FinOps, Keamanan & Operasi
- **Akses & Kunci:** Terbitkan Kunci Virtual per lingkungan, putar dengan masa tenggang 24 jam, atau cabut bertahap maupun darurat. Kelola 6 peran RBAC.
- **FinOps:** Pantau biaya eksak, estimasi, tahan aktif, dan permintaan tidak terukur; atur batas anggaran mikro-USD dan ekspor laporan CSV/JSON.
- **Keamanan:** Tinjau persetujuan HITL untuk panggilan MCP destruktif, aktifkan/cabut Pemutus Darurat 4 tingkat, dan verifikasi rantai hash audit RFC 8785 JCS.
- **Operasi (peran `OPERATOR`):** Pantau `/readyz` dan epoch klaster, buat cadangan terenkripsi `.tpop.enc`, jalankan pra-pemeriksaan pembaruan, dan kelola webhook bertanda tangan HMAC.

---

## 4. Ringkasan Perintah CLI

| Perintah | Fungsi |
| --- | --- |
| `tanpopo serve` | Menjalankan server Tanpopo (`--role all`, `control-plane`, atau `data-plane`). |
| `tanpopo doctor` | Memeriksa file lokal, database, dan kesehatan layanan (`--output json`). |
| `tanpopo support bundle` | Membuat arsip diagnostik tersensor (tanpa rahasia atau prompt). |
| `tanpopo admin reset-owner` | Mengatur ulang kata sandi pemilik lokal secara offline. |
| `tanpopo backup create / verify / restore` | Membuat, memverifikasi, dan memulihkan cadangan `.tpop.enc`. |
| `tanpopo plugin init / validate / test / pack` | Mengembangkan, menguji offline, dan mengemas ekstensi `.tpkg`. |
| `tanpopo upgrade preflight` | Memvalidasi kompatibilitas ABI sebelum memperbarui Tanpopo Core. |
| `tanpopo key / route / budget / audit / killswitch` | Mengelola sumber daya control plane jarak jauh melalui `/admin/v1`. |
