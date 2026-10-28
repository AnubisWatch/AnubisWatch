# Binary ve Docker tamamlanma sonucu

26 Ekim 2026. Kullanıcının netleştirdiği hedef, kalıcı veriyle çalışan standalone
binary veya Docker kurulumudur. Bu kapsam için yerel sürüm adayı hazırlandı ve
doğrulandı. Kubernetes ve yeni HA özellikleri bu kabulün koşulu değildir.

İlk [mimari inceleme](2026-10-26-project-completion-review.md) başlangıç durumunu
belgeler. Aşağıdaki sonuçlar o incelemeden sonraki düzeltmelere ve son adayın
gerçek çalıştırılmasına aittir. Üretim hostuna kurulum, Git push ve registry
yayını yapılmadı.

## Kapatılan sorunlar

| Kanıt | Değişiklik | Regresyon koruması |
| --- | --- | --- |
| F89 | Aynı Soul/timestamp için farklı Judgment ID'leri korunur. Yeni anahtar timestamp/ID içerir; legacy kayıtlar okunur ve aynı legacy ID yeniden yazıldığında çoğalmaz. HA tekrar uygulaması ID'ye göre ayırt edilir. | Şifreli/şifresiz, raw/rollup, restart, legacy, retention, eşzamanlı yazı ve replicated replay testleri |
| F90 | Journey edit düğmesinin hidden niteliği doğru JSX konumuna taşındı. | Viewer/editor/owner arayüz testi |
| F91 | Private/protected HTML, JSON, badge, widget, feed, parola formu ve redirect cevapları shared cache'e kapatıldı. | Görünürlük, format ve oturum matrisi |
| F92 | Private sayfalar dashboard'un auth_token cookie'sini kullanır. Legacy cookie desteklenir; geçersiz canonical cookie legacy ile geçersiz kılınamaz. | Gerçek login cookie jar, anonim ve başka workspace kontrolleri |
| F93 | CLI sürümü metrics/tracing'e bağlandı; release Docker build'leri version/commit/timestamp alır. | Gerçek server metrics testi ve container identity kontrolü |
| F94 | Standalone Compose, kapalı cluster profili için cluster secret istemez. Runtime cluster güvenlik kontrolleri korunur. | Eksik cluster environment ile Compose parse testi |
| F95 | Launcher mevcut config'i korur; foreground exec kullanır ve başka süreçleri süreç adına göre durdurmaz. | İzole mock süreç araçlarıyla ownership ve config koruma testi |
| F96 | Simple/interactive init, kimlik bilgisi taşıyan yeni dosyaları 0600 ile oluşturur. İlk parola environment ile sağlanabilir. | Umask 022 altında gerçek init dosya izni testi |
| F97 | Başarılı parola POST'u, `//...` istek yolundan başka origin'e yönlendiremez. | Normal ve saldırgan yönlendirme yolu testi |

F89–F97'nin değiştirilmemiş ilk hata kanıtları son kaynak üzerinde yeniden
çalıştırıldı ve dokuzu da `PROBLEM NOT REPRODUCED` ile geçti. F89–F96 için
verifier'lar üçer kez geçti; F97 verifier'ı da üç kez geçti. Kritik durumlar
kalıcı testlere taşındı.

Tam statik kontrolde bulunan kalite engelleri de kapatıldı: auth cleanup
persist hataları açıkça loglanıyor, kullanılmayan eski trace-status helper'ı
kaldırıldı, yalnız testlerin kullandığı summary helper test dosyasına taşındı,
soak aracının context ve hata işleme yolları düzeltildi. Uygulama çapında yeni
güvenlik kuralı istisnası eklenmedi.

Race koşusunda düşen Raft testi log mesajını applied-index yayınının tamamlanması
sanıyordu. Assertion gevşetilmedi; mevcut applyMu bariyeriyle işlemin bitmesi
bekleniyor. İlgili Raft ve redirect regresyonları race açıkken 20 tekrar geçti.

## Son adayın kimliği

| Alan | Değer |
| --- | --- |
| Version | `candidate-20261026` |
| Base commit | `3653a0a1d137adc957f5e30e25d036d10fbc3cfb` ve açıkça dirty çalışma ağacı |
| Build tarihi | `2026-10-26T09:09:02Z` |
| Linux amd64 binary SHA256 | `fade80a89e7783cec02912e5895583571ad8211d10d49975de015586e9f4955c` |
| Yerel Docker tag | `anubiswatch:completion-20261026-final` |
| Docker image kimliği | `sha256:29fdcb1a348c559ba803374cfe9a472e8b7fde968abe1e41514e802d9f4eea15` |

Paketler `.temp_files/completion-fixes-round1/final-candidate/` altında:

- `anubis-linux-amd64.tar.gz`: executable, standalone config, kurulum rehberi ve lisans.
- `anubiswatch-docker.tar`: `docker load` ile içeri alınabilen yerel image.
- `source-review.tar.gz`: intended kaynak, yeni/eski regresyonlar, gömülü dashboard ve dokümanlar.
- `checksums.txt`: binary, paket, source archive ve Docker export checksum'ları.
- `source-hashes.json` ve `source-fingerprint.txt`: kaynak paketinin dosya envanteri ve kimliği.
- `candidate-identity.json`: build, binary ve image kimliğini kabul sonuçlarına bağlar.

`.temp_files` Git tarafından ignore edilir; dosyalar yeni checkout'a kendiliğinden
taşınmaz. Kaynak paketine runtime verisi, .env, özel anahtarlar, kişisel config
ve scratch kanıtları alınmadı. Yerel image tag'i yayımlanmış registry referansı
olarak sunulmuyor.

## Doğrulama sonuçları

| Kontrol | Sonuç |
| --- | --- |
| `go build ./...` | PASS |
| `go test ./...` | PASS |
| `go vet ./...` | PASS |
| Tam race/coverage gate | PASS: 19.032/22.046 statement, **%86,33**; minimum %80 |
| gofmt ve diff whitespace | PASS |
| Pinned golangci-lint v2.15.0 | PASS: 0 issue |
| Pinned gosec v2.30.0 | PASS: 95 kaynak dosyası, 0 bulgu ve 0 paket analiz hatası |
| govulncheck v1.9.0, güncel toolchain | PASS: 0 erişilebilir açık; required modülde 1 çağrılmayan advisory |
| Frontend lint/build/embed | PASS |
| Vitest coverage | PASS: 94 dosya, **605 test**; statement %97,59, branch %93,41 |
| Playwright Chromium | PASS: **49/49** |
| pnpm audit high | PASS: bilinen açık bulunmadı; test zincirindeki undici 8.12.3 |
| Launcher, Compose ve shell kontrolleri | PASS |
| Mevcut offline Kubernetes drill testleri | PASS; canlı Kubernetes kabulü anlamına gelmez |
| Son binary ile izole smoke | PASS |
| Son Docker image ile lifecycle ve cold recovery | PASS: **28 kontrol** |
| Son Docker export üzerinde Trivy | PASS: HIGH/CRITICAL + ignore-unfixed filtresinde 0 bulgu |

gosec v2.29.0'ın parser'ı Go1.27 export verisini okuyamadığı için eski araç
koşusu başarı sayılmadı. CI aynı Go toolchain ile v2.30.0'ı çalıştıracak şekilde
düzeltildi. Scratch klasörleri analiz kapsamından çıkarıldı; ürün kaynakları
ve mevcut severity/confidence eşikleri korunuyor. Araç sürümünün Go1.27.3
güncellemesi [resmî sürüm notlarında](https://github.com/securego/gosec/releases/tag/v2.30.0)
yer alıyor.

## Binary yük ve yeniden başlatma kabulü

Son binary, izole loopback HTTP hedeflerine **1.000 Soul, 1 saniye aralık,
180 saniye** yükle çalıştırıldı. Yük ortasında hedef arızası ve iyileşmesi
enjekte edildi; şifreli veriyle temiz restart yapıldı.

- **183.774** Judgment saklandı; Soul başına 183–184 sonuç.
- **1.000/1.000** Soul'da healthy → dead → healthy geçişi kaydedildi.
- Örneklenen **1.000 Judgment ID'si** restart sonrası korundu.
- En yüksek RSS **520,1 MiB**; test sınırı 768 MiB.
- Gözlenen tepe hedef concurrency 20.

Bu, tanımlı fixture için kısa süreli kabul sonucudur. Temsili çoklu protokol
yükü, günlerce retention/compaction dengesi veya production SLO garantisi olarak
genişletilmemeli.

## Docker kurulum ve kurtarma kabulü

Son image; non-root süreç, read-only root filesystem, geçici çalışma dizini ve
şifreli named volume ile çalıştırıldı. Port dinamik olarak keşfedildi.
Kontrollü yerel HTTP hedefi ve webhook sink'i kullanıldı; gerçek kullanıcıya
veya dış bildirim kanalına mesaj gönderilmedi.

28 kontrol; authentication ve korunan metrics, normal login ile private status
ve no-store, gerçek HTTP probe, ayrı failure/recovery kurallarıyla webhook'a
dead/alive teslimi, manuel Journey, parola değişikliği ve restart'ı kapsadı.
Writer durdurulduktan sonra veri ayrı boş volume'a kopyalanıp aynı key/image ile
açıldı. Eski parola reddedildi, değişmiş parola kabul edildi; örneklenen Judgment
ID'leri ve manuel Journey kaydı korundu, monitoring yeniden çalıştı. Testin
sahip olduğu container'lar ve volume'lar temizlendi.

İlk iki Docker fixture denemesi, recovery bildirimi için yanlış rule tipi
beklediği için geçersizdi. Kayıtları korunuyor. Son kabul, public API'nin
desteklediği `status_change: dead → alive` kuralıyla yapıldı; bunlar ürün hatası
olarak raporlanmıyor.

## Kurulum ve kapsam

[Binary ve Docker kurulum rehberi](../deployment/binary-and-docker.md) güncel
komutları, private CIDR politikasını, retention ve veri formatı sınırlarını
tanımlar. `configs/standalone.anubis.yaml` explicit authentication kullanır ve
başlatmadan önce `ANUBIS_ADMIN_PASSWORD` ister. Alternatif launcher/init yolu,
eksik config için başlangıç parolasını üretir.

Yeni Judgment anahtarları ve WAL/snapshot formatları nedeniyle eski writer'ı
yeni veri dizininde açmak rollback değildir. Pre-upgrade yedeği ayrı boş
storage'a, matching binary ve key ile geri yüklemek gerekir.

Tenant kotaları, dashboard white-label branding, global escalation ayarı,
status subscription confirmation/delivery, browser synthetic runtime, keyfi
HA ölçekleme ve HA OIDC/LDAP bu standalone sürümün desteklenen kapsamına
eklenmedi. Mevcut uyarılar/guard'lar korunuyor; rule-level escalation ve
workspace resource isolation mevcut. Advanced Journey alanları YAML/JSON/API
üzerinden kullanılabilir; görsel editörün kapsamı daha dardır.

## Çalışma ağacı ve kalan yayın sınırı

Başlangıçtaki modified/untracked dosyalar hash ve içerik kopyalarıyla korundu.
Yalnız bu düzeltmelerin gerektirdiği dosyalar değiştirildi; kayıp dosya yok.
İnceleme manifesti, önceki iş ile bu turdaki değişiklikleri ayrı gösterir.
Commit, reset, stash veya başka bir çalışma ağacını temizleme yapılmadı.

Yerel binary/Docker kabulü tamamlandı. Uzak CI henüz bu dirty ağacı çalıştırmış
değil; registry publish, gerçek hosta deployment, kullanıcıya ait offsite backup
ve kararlaştırılmış uzun süreli kapasite/RPO/RTO ölçümleri yapılmadı. Bu son
işler için yayımlanacak kaynak/release kimliği ve gerçek işletim hedefi gerekir.

Ham kanıtlar `.temp_files/completion-fixes-round1/` altında; finding komutları,
before/after sonuçları ve sınırlar `.temp_files/ledger_completion-review.md`
içinde korunuyor.
