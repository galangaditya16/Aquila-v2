# RSISA Print Server

**Author: CV ANAK BANGSA**

JL. KI JAGAPATI NO. 22 RT. 02 RW. 01, KEL. KRANDEGAN, KEC. / KAB. BANJARNEGARA.

© 2026 CV ANAK BANGSA. Seluruh hak dilindungi.

Service lokal lintas platform untuk PC tempat aplikasi klien digunakan. Halaman tes membaca antrean printer dari sistem operasi dan mengirim lembar uji kualitas ke printer yang dipilih. Aplikasi klien mengakses service lokal ini dari browser PC.

## Alur

Browser di PC membuka aplikasi klien dari VPS dan mengirim request ke `http://127.0.0.1:17891` pada PC yang sama. Print server lokal membaca antrean yang dikenal OS di PC tersebut. VPS tidak perlu membuka koneksi ke IP printer atau PC.

## Dukungan OS dan printer

Satu source Go dapat dibangun untuk Windows, Linux, dan macOS. Pendeteksian memakai Windows Print Spooler di Windows dan CUPS di Linux/macOS. Halaman tes mengirim uji karakter, font, garis, dan angka ke antrean yang dipilih, termasuk printer jaringan atau USB yang sudah terpasang di OS.

## Aplikasi desktop Aquila

Aquila adalah aplikasi desktop dengan baki sistem (system tray) dan panel kendali yang membungkus print server yang sama. Tersedia sebagai installer untuk macOS (DMG), Windows (setup.exe NSIS), dan Linux (deb/rpm/AppImage). Petunjuk CLI tetap tersedia di bagian berikutnya.

### Instalasi

1. Pasang aplikasi dari installer. Pada macOS, buka `aquila.dmg` lalu seret **Aquila** ke folder **Applications**.
2. Saat pertama dijalankan, Aquila otomatis membuat `print-server.json` dengan nilai awal `listen: 127.0.0.1:17891` dan `allowed_origins: ["https://app.example.org", "http://localhost:4200"]`. Tidak perlu menyalin `print-server.example.json` lagi.
3. Buka tab **Alamat Diizinkan** dan ganti origin contoh dengan alamat aplikasi klien. Domain, subdomain, dan host dengan port khusus (misal `http://192.168.10.25:8080`) didukung.
4. Layanan print server dimulai otomatis saat aplikasi dibuka (bisa diubah di tab **Pengaturan**).

Jika muncul pesan bahwa `127.0.0.1:17891` sedang dipakai, tutup Aquila lain yang masih berjalan. Jika port dipakai aplikasi lain, ubah port di **Pengaturan → Alamat Dengarkan** dan sesuaikan URL print server pada aplikasi klien. Aquila tidak menghentikan aplikasi lain atau mengganti port secara otomatis.

### Baki sistem

- **Klik kiri** ikon baki: popup ringkas berisi status layanan, tombol mulai/hentikan, dan pintasan ke panel.
- **Klik kanan** ikon baki: menu status, **Buka Panel**, **Mulai/Hentikan Layanan**, dan **Keluar**.
- Ikon baki berubah mengikuti keadaan layanan: berhenti, berjalan, atau bermasalah.
- Menutup jendela panel tidak mematikan layanan; Aquila tetap berjalan di baki sistem.

### Lokasi konfigurasi aplikasi desktop

- macOS: `~/Library/Application Support/Aquila/`
- Windows: `%AppData%\Aquila\`
- Linux: `~/.config/Aquila/`

Folder ini berisi `print-server.json` (konfigurasi layanan, format sama dengan CLI) dan `app-settings.json` (preferensi aplikasi desktop seperti autostart dan visibilitas jendela). Variabel lingkungan `RSISA_PRINT_SERVER_CONFIG` tetap dihormati untuk menunjuk file konfigurasi di lokasi lain.

## Cara memasang per PC (CLI)

Setelah repo `rsisa-print-server` diterbitkan, unduh binary sesuai OS dan arsitektur CPU dari halaman **Releases** repo tersebut. Belum ada tautan rilis sampai repo jarak jauh dibuat dan asetnya diunggah.

1. Letakkan binary di folder khusus PC. Untuk Windows gunakan `rsisa-print-server.exe`; untuk Linux/macOS gunakan `rsisa-print-server` dan jalankan `chmod +x rsisa-print-server`.
2. Salin `print-server.example.json` dari repo ini sebagai `print-server.json` di folder binary. Ganti `https://app.example.org` dengan origin frontend VPS yang benar (tanpa path seperti `/login`).
3. Pastikan printer sudah terpasang dan aktif pada OS PC. Jalankan binary dan periksa `http://127.0.0.1:17891/health`.
4. Buka **Sistem → Printer → Tes Printer**, pilih printer, jenis kertas, dan mode, lalu tekan **Cetak halaman uji**.

Jika `print-server.json` sudah disesuaikan oleh IT, pertahankan file itu saat memperbarui binary. Browser mengirim request ke print server lokal PC; port ini hanya bind ke loopback.

## Build binary

Dari root proyek ini, jalankan `go test ./...` lalu `go build -o rsisa-print-server ./cmd/print-server`. Untuk membuat aset rilis lintas platform secara manual:

```sh
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/rsisa-print-server-windows-amd64.exe ./cmd/print-server
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/rsisa-print-server-linux-amd64 ./cmd/print-server
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o dist/rsisa-print-server-linux-arm64 ./cmd/print-server
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/rsisa-print-server-macos-amd64 ./cmd/print-server
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o dist/rsisa-print-server-macos-arm64 ./cmd/print-server
```

Unggah binary yang sudah diuji ke **Releases** repo terpisah. Aset `dist/` diabaikan oleh Git. Nama binary Unix di aset rilis memiliki akhiran OS/arsitektur; setelah diunduh, ubah namanya menjadi `rsisa-print-server` agar sesuai petunjuk instalasi.

### Build aplikasi desktop Aquila

Aplikasi desktop dibangun dari folder `desktop/` memakai [Wails v3](https://v3.wails.io). Pastikan `wails3` CLI tersedia (misal `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0`) dan Node.js terpasang untuk membangun frontend:

```sh
cd desktop
wails3 task build              # frontend + binary desktop
wails3 task package            # macOS: bundel aquila.app; Windows: setup.exe NSIS; Linux: deb/rpm/AppImage
wails3 task darwin:package:dmg # macOS: installer aquila.dmg
```

Task packaging harus dijalankan pada host dengan OS target (NSIS butuh Windows; deb/rpm/AppImage butuh Linux). Hasil build berada di `desktop/bin/`. Untuk mengembangkan UI dengan hot reload, jalankan `npm run dev` di `desktop/frontend` sambil menjalankan `wails3 task dev`.

Untuk uji lokal macOS, salin hasil ke folder `output/` di root repo (diabaikan Git):

```sh
mkdir -p ../output
ditto bin/aquila.app ../output/aquila-1.0.5-testing.app
cp bin/aquila.dmg ../output/aquila-1.0.5-testing.dmg
```

### Uninstall Aquila

Pilih **Pengaturan → Uninstall Aquila** dan konfirmasi. macOS memindahkan aplikasi ke Trash; Windows membuka uninstaller NSIS yang menutup proses Aquila agar port server dilepas; Linux menghapus paket deb/rpm setelah izin administrator diberikan. Uninstaller mempertahankan `print-server.json` dan `app-settings.json` agar konfigurasi bisa dipakai kembali saat instal ulang. Untuk versi portable, pilih **Keluar** dari menu klik kanan ikon tray terlebih dahulu, lalu hapus file; menghapus file saat proses masih berjalan tidak menghentikan server.

## Rilis dan distribusi installer

### Lokasi aset rilis

- **GitHub**: halaman **Releases** repo [`karya-anakbangsa/aquila-printers-discover`](https://github.com/karya-anakbangsa/aquila-printers-discover) — inilah "folder assets" yang dibagikan ke pengguna (persis seperti halaman Releases pada umumnya).
- **Lokal**: folder `dist/` di root repo (diabaikan Git). Bila Anda merakit installer sendiri, salin hasilnya ke `dist/` dengan nama berikut:

```
dist/
├── Aquila-macOS-universal.dmg
├── Aquila-macOS-universal.pkg
├── Aquila-windows-amd64-installer.exe
├── Aquila-windows-amd64.exe
├── aquila_<versi>_amd64.deb
├── aquila-<versi>-1.x86_64.rpm
├── Aquila-linux-amd64.tar.gz
└── checksums.txt
```

### Rilis otomatis via GitHub Actions

Setiap pendorongan tag `v*` memicu workflow [`.github/workflows/release.yml`](.github/workflows/release.yml) yang merakit seluruh installer (macOS universal dmg+pkg, Windows NSIS+portable, Linux deb+rpm+tar.gz), membuat `checksums.txt`, lalu menerbitkannya ke halaman Releases.

Langkahnya:

```sh
# 1. Pastikan semua perubahan sudah ter-commit di branch main
git status

# 2. Tambahkan remote repo kantor (pakai SSH, bukan HTTPS)
git remote add origin git@github.com:karya-anakbangsa/aquila-printers-discover.git

# 3. Dorong kode ke repo
git branch -M main
git push -u origin main

# 4. Buat dan dorong tag versi (contoh: v1.0.6)
git tag v1.0.6
git push origin v1.0.6

# 5. Tunggu Actions selesai → halaman Releases otomatis terisi semua installer
```

Catatan penting:

- **Akses SSH**: remote memakai `git@github.com:...` sehingga memakai kunci SSH `~/.ssh/id_ed25519` yang terautentikasi sebagai akun GitHub Anda. Untuk repo organisasi ini, pastikan akun tersebut (mis. `dayatfadila7`) sudah ditambahkan sebagai **kolaborator** dengan akses *Write* ke `karya-anakbangsa/aquila-printers-discover`. Cek dengan `ssh -T git@github.com` dan `git ls-remote git@github.com:karya-anakbangsa/aquila-printers-discover.git`.
- **Identitas commit**: konfigurasi git global komputer ini memakai akun pribadi. Bila proyek kantor butuh identitas berbeda, set per-repo saja: `git config user.name "Nama Anda"` dan `git config user.email "email@kantor"`.
- **Naikkan versi** sebelum men-tag: edit `version` di `desktop/build/config.yml` lalu jalankan `wails3 task common:update:build-assets` dari folder `desktop/` (menyinkronkan Info.plist, NSIS, dan metadata paket). Workflow juga menyamakan versi paket deb/rpm dengan nomor tag otomatis.
- **Menjalankan workflow tanpa merilis**: jalankan workflow **Release** secara manual (Actions → Release → Run workflow) untuk merakit artefak percobaan tanpa menerbitkan rilis.
- **Catatan untuk pengguna macOS**: build di-sign ad-hoc; peluncuran pertama perlu klik kanan → **Open** untuk melewati Gatekeeper. Sertakan catatan ini saat membagikan tautan unduhan.

## Konfigurasi setiap PC

Salin `print-server.example.json` sebagai `print-server.json` di sebelah binary. Ganti origin contoh dengan alamat aplikasi klien di VPS. Isi origin saja, tanpa path; contoh `https://app.example.org`. Pertahankan `http://localhost:4200` hanya untuk development jika diperlukan.

Halaman tes membaca printer dari PC. Untuk cetakan aplikasi, daftar printer dan tujuan cetak dikelola di aplikasi klien. Saat mencetak, browser mengambil tujuan tersebut dari API dan meneruskannya ke print server lokal; pengguna tidak perlu mengedit `printers` atau `routes` di JSON. Konfigurasi route lokal lama tetap didukung untuk pemanggil API manual yang belum mengirim tujuan cetak.

Print server hanya bind ke `127.0.0.1:17891`; jangan ubah ke alamat LAN. Setelah menjalankan service, cek `http://127.0.0.1:17891/health`. Browser mungkin meminta izin akses jaringan lokal untuk situs aplikasi klien; izinkan untuk memakai print server PC tersebut.

### Windows

Letakkan `.exe` dan konfigurasi di satu folder. Jalankan `rsisa-print-server.exe`. Untuk auto-start, buat Scheduled Task dengan trigger **At log on** untuk akun pengguna PC dan executable tersebut sebagai program.

### Linux

Letakkan binary dan konfigurasi di folder yang sama, lalu beri izin eksekusi:

```sh
chmod +x rsisa-print-server
./rsisa-print-server
```

Untuk menjalankannya sebagai service systemd, buat `/etc/systemd/system/rsisa-print-server.service`:

```ini
[Unit]
Description=RSISA local print server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/rsisa-print-server
ExecStart=/opt/rsisa-print-server/rsisa-print-server
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
```

Simpan binary dan `print-server.json` di `/opt/rsisa-print-server`, lalu jalankan `sudo systemctl daemon-reload && sudo systemctl enable --now rsisa-print-server`. Lihat log dengan `journalctl -u rsisa-print-server -f`.

### macOS

Letakkan binary dan konfigurasi di folder yang sama, lalu jalankan:

```sh
chmod +x rsisa-print-server
./rsisa-print-server
```

Untuk auto-start, buat file `~/Library/LaunchAgents/id.rsisa.print-server.plist` (ubah path binary sesuai folder instalasi):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>id.rsisa.print-server</string>
  <key>ProgramArguments</key><array><string>/usr/local/lib/rsisa-print-server/rsisa-print-server</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict></plist>
```

Muat dengan `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/id.rsisa.print-server.plist`.

## API

- `GET http://127.0.0.1:17891/health` — status layanan.
- `GET http://127.0.0.1:17891/printers` — daftar antrean printer OS, status, driver, port, dan printer default.
- `GET http://127.0.0.1:17891/printers/discover` — printer jaringan yang mengiklankan IPP/IPPS lewat Bonjour/mDNS.
- `POST http://127.0.0.1:17891/printers/test` — JSON `{"name":"nama antrean","paper":"A4","mode":"bw"}` untuk mencetak lewat antrean OS, atau sertakan `uri` printer IPP yang mendukung PDF. Untuk CF, LABEL, atau LAINNYA, sertakan `width_mm` dan `height_mm`.
- `POST http://127.0.0.1:17891/print/{document_type}?module_code={module}` — body PDF mentah, `Content-Type: application/pdf`.

PDF tidak dikirim ke TCP 9100: beberapa printer mencetak byte `%PDF` sebagai banyak halaman karakter acak. Di macOS/Linux, pilih antrean OS yang ditemukan di halaman pengaturan (contoh `ipp://127.0.0.1:631/printers/EPSON_L5190_Series`). Alamat `127.0.0.1` merujuk ke antrean CUPS di PC ini, bukan IP jaringan printer; Aquila mengirim PDF melalui utilitas `lp` ke antrean OS. CUPS meneruskan dokumen ke alamat perangkat yang dikonfigurasi pada sistem operasi.

### Pengecualian sertifikat IPPS untuk pengujian

Jika printer memakai sertifikat yang belum dipercaya, jalankan binary dengan pengecualian untuk satu IP lokal dan port yang tepat:

```sh
RSISA_INSECURE_IPPS_HOST=192.168.8.108:631 ./rsisa-print-server
```

Pengecualian ini hanya berlaku untuk IPPS pada alamat tersebut, tidak mengikuti redirect, dan mati saat variabel tidak diberikan. Koneksi tetap terenkripsi, tetapi identitas printer tidak diverifikasi; gunakan hanya untuk pengujian pada jaringan tepercaya. Pengecualian tidak menambahkan dukungan PDF. Printer yang hanya menerima PWG Raster/URF/JPEG tetap perlu antrean driver OS untuk mencetak PDF.

Contoh:

```sh
curl -X POST 'http://127.0.0.1:17891/print/etiket_obat?module_code=resep_rajal' \
  -H 'Content-Type: application/pdf' --data-binary @etiket.pdf
```

`202 Accepted` berarti PDF sudah diteruskan ke printer; belum memastikan kertas sudah keluar. Jika koneksi terputus setelah pengiriman dimulai, periksa printer sebelum mencoba ulang agar tidak mencetak ganda.

## Hubungkan aplikasi klien

Aplikasi klien memakai `VITE_PRINT_SERVER_URL`, dengan default `http://127.0.0.1:17891`. Untuk VPS, URL ini tetap alamat loopback karena request dikirim browser ke print server pada PC pengguna. Tambahkan origin VPS ke `allowed_origins` di konfigurasi print server setiap PC.
