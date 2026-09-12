# Nexus Repository 功能對照表

> 目的：確保「Nexus 有的我們都有」是可追蹤的清單，而不是一句話。
> 基準：Nexus Repository 3.95 Community Edition（公司環境）。Pro-only 功能另列，作為超越 Nexus CE 的機會。
> 狀態欄：`P0`–`P7` 對應 `holiaokho-architecture-draft.md` §14 的實作階段；`後` = 有排但無階段；`不做` = 明確不做並附理由；✅ = 已實作並測試（2026-09-13 夜）。
> 2026-09-13 初版。

---

## 1. Repository 格式

Nexus 3.95 CE 支援的格式（從公司 DB 的 `<fmt>_component` 表確認）：

| 格式 | Nexus | Holiaokho | 備註 |
|---|---|---|---|
| Maven 2 | ✅ | ✅ P1 | hosted/proxy/group，mvn 實測 |
| npm | ✅ | ✅ P2 | hosted/proxy/group，npm 實測 |
| Docker | ✅ | ✅ P3 | 含 daemon registry-mirror 模式，dockerd 實測 |
| OCI | ✅（3.9x 新增，與 Docker 分開） | **P3** | 與 Docker 同一 plugin，含 referrers、cosign |
| NuGet（V2 + V3） | ✅ | **P6** | 公司有建 repo 但沒在用 |
| PyPI | ✅ | **P6** | |
| Raw | ✅ | ✅ | hosted/proxy/group |
| Helm | ✅ | **P6** | |
| Go | ✅ | **P6** | |
| RubyGems | ✅ | 後 | |
| APT | ✅ | 後 | 需 GPG 簽章 |
| YUM | ✅ | 後 | 需 GPG 簽章、createrepo 邏輯 |
| Conda | ✅ | 後 | |
| Conan | ✅ | 後 | |
| R (CRAN) | ✅ | 後 | |
| CocoaPods | ✅ | 後 | |
| Cargo | ✅ | 後 | |
| Composer | ✅ | 後 | |
| Git LFS | ✅ | 後 | |
| p2 (Eclipse) | ✅ | 後 | |
| Hugging Face | ✅ | 後 | |
| Swift | ✅ | 後 | |
| Alpine (apk) | ✅ | 後 | |
| Ansible Galaxy | ✅ | 後 | |
| pub (Dart) | ✅ | 後 | |
| Terraform | ✅ | 後 | |

Plugin 介面在 P0 就要定好，讓「後」的格式可以由社群貢獻。

## 2. Repository 型態與行為

| 功能 | Nexus | Holiaokho |
|---|---|---|
| hosted / proxy / group | ✅ | ✅ 引擎層通用 |
| Proxy：contentMaxAge / metadataMaxAge | ✅ | ✅ |
| Proxy：negative cache + TTL | ✅ | ✅ |
| Proxy：上游認證（Basic / Bearer / preemptive） | ✅ | **P1** |
| Proxy：自訂 CA / trust store、outbound HTTP proxy、連線逾時、重試、autoBlock | ✅ | **P1** |
| Proxy：blocked（暫停上游） | ✅ | ✅（含 autoBlock、stale-if-error） |
| Group：成員排序、first-match、metadata merge | ✅ | ✅ |
| Group：deploy 到 group（轉發到第一個 hosted） | ✅ | **P1** |
| Hosted：writePolicy（ALLOW / ALLOW_ONCE / DENY） | ✅ | ✅ |
| Maven：layoutPolicy STRICT/PERMISSIVE、versionPolicy RELEASE/SNAPSHOT/MIXED、contentDisposition | ✅ | ✅（contentDisposition 未做） |
| Maven：snapshot 時間戳版本、`maven-metadata.xml` 產生與 merge、checksum 旁邊檔 | ✅ | ✅ |
| Maven：rebuild metadata 任務、maven-indexer | ✅ | **P1** / 後 |
| Docker：httpPort / httpsPort connector、subdomain connector、path 模式 | ✅ | ✅ httpPort、path；httpsPort／subdomain 未做（TLS 交給反向代理） |
| Docker：v1 API | ✅（預設關） | 不做（Docker 已棄用） |
| Docker：forceBasicAuth、Bearer token realm | ✅ | ✅ |
| Docker：foreign layer 快取、index type（HUB / registry / custom） | ✅ | **P3** |
| Docker：group 支援 push（Pro） | Pro | 後（我們可以做） |
| Routing rules（allow / block regex） | ✅ | **P4** |
| Content selectors（CSEL 表達式，配合權限） | ✅ | **P5** |
| Strict content type validation | ✅ | **P1** |
| Online / offline 切換 | ✅ | ✅ |
| Repository 層級的 cleanup policy 掛載 | ✅ | **P4** |

## 3. 儲存

| 功能 | Nexus | Holiaokho |
|---|---|---|
| File blob store | ✅ | ✅ |
| S3 blob store（含 IAM role、encryption、prefix） | ✅ | ✅（MinIO 實測；IAM role 走 SDK 預設鏈） |
| Azure Blob store | ✅ | 後 |
| Google Cloud Storage | ✅ | 後 |
| Group blob store（多 blob store 聚合） | Pro | 後 |
| Blob store soft-delete + 硬刪任務 | ✅ | **P4** |
| Blob store quota 與警示 | ✅ | **P4** |
| Blob store 容量／指標 | ✅ | **P4** |
| 跨 repo 去重（content-addressable） | ❌（Nexus 不去重） | ✅ 內建，**超越 Nexus**（Docker layer 跨 repo 不重抓） |
| Change blob store（搬 repo 到另一個 store） | ✅ | 後 |

## 4. 安全與權限

| 功能 | Nexus | Holiaokho |
|---|---|---|
| Local users（bcrypt/Shiro） | ✅ | ✅ argon2id + Shiro1 驗證後自動升級 |
| Roles / privileges（repo-view、repo-admin、application、wildcard、script） | ✅ | **P0** 基礎、**P5** 完整 |
| Anonymous access 開關 + 匿名角色 | ✅ | ✅ |
| LDAP / Active Directory | ✅ | **P5** |
| SAML | Pro | **P5**（我們做 OIDC，另評估 SAML） |
| OIDC | ❌（Nexus 沒有） | **P5**，**超越 Nexus** |
| User tokens（給 CI 的長效 token） | Pro | ✅ **超越 Nexus CE** |
| Docker Bearer token realm | ✅ | ✅ |
| npm Bearer token realm（`npm login`） | ✅ | ✅ |
| NuGet API key realm | ✅ | **P6** |
| Conan / Hugging Face / Pub / Terraform / Ansible Galaxy token realm | ✅ | 後（隨對應格式） |
| OCI Bearer token realm（與 Docker 分開） | ✅ | **P3** |
| 登入失敗限流（連續失敗回 429） | ✅ | ✅（預設 10 次／分鐘，依 IP） |
| Secret 加密金鑰（`nexus.secrets.file`）、金鑰輪替與 re-encryption 任務 | ✅ | **P4**（proxy 上游密碼、LDAP 密碼、SMTP 密碼等要加密存放，金鑰可換） |
| Privilege 類型：wildcard / application / repository-admin / repository-view / script | ✅（預設 365 個） | **P5** 同模型（去掉 script） |
| Rut Auth（反向代理 header 認證） | ✅ | **P5** |
| Default Role realm | ✅ | **P5** |
| Realm 啟用順序設定 | ✅ | **P5** |
| SSL trust store（匯入上游憑證） | ✅ | **P1** |
| Content selector 綁權限 | ✅ | **P5** |
| Password 政策、帳號鎖定 | 部分 | **P5** |
| Audit log | ✅（capability） | ✅ 基本（管理操作） |

## 5. 維護任務（Tasks）

Nexus 內建的 scheduled tasks：

| 任務 | Nexus | Holiaokho |
|---|---|---|
| Cleanup policies 執行（依 lastDownloaded / lastBlobUpdated / regex / 保留 N 版） | ✅ | ✅ 基本 |
| Cleanup 預覽（dry-run） | ✅ | **P4** |
| Admin - Compact blob store（硬刪 soft-deleted） | ✅ | **P4** |
| Admin - Delete blob store temporary files | ✅ | **P4** |
| Admin - Export databases for backup | ✅ | **P7** |
| Admin - Log database table record counts | ✅ | 不做（用 metrics 取代） |
| Admin - Remove a member from a blob store group | Pro | 後 |
| Repair - Rebuild repository browse | ✅ | **P7** |
| Repair - Rebuild repository search | ✅ | **P7** |
| Repair - Reconcile component database from blob store | ✅ | **P7**（索引重建） |
| Repair - Rebuild Maven repository metadata | ✅ | ✅（刪除套件後自動；無獨立任務） |
| Repair - Reconcile npm /-/v1/search metadata | ✅ | **P2** |
| Repair - Rebuild Yum/APT metadata | ✅ | 後 |
| Docker - Delete unused manifests and images | ✅ | **P4** |
| Docker - ECR token refresh（proxy AWS ECR 時自動換 token） | ✅ | 後 |
| Cleanup unused `<format>` blobs（每格式一個，內建自動排程） | ✅ | ✅ blob GC 統一做 |
| System - Repository Health Check（RHC，連 Sonatype 漏洞資料） | ✅（連 IQ） | 不做（同 IQ） |
| Malicious Risk on Disk 自動啟用 RHC | ✅（3.7x+） | 不做（同 IQ） |
| Docker - Delete incomplete uploads | ✅ | **P3** |
| Maven - Delete SNAPSHOT、Delete unused SNAPSHOT、Remove snapshots from group、Purge unused | ✅ | **P4** |
| Maven - Publish Maven Indexer files | ✅ | 後 |
| Statistics - Recalculate vulnerabilities | IQ | 不做 |
| Task：cron 排程、手動觸發、執行記錄、通知 email | ✅ | **P4** |
| Task：多節點下的分散式 lock | Pro | 後（HA 時一起做） |

## 6. 管理與系統

| 功能 | Nexus | Holiaokho |
|---|---|---|
| Web UI：browse（樹狀）、search、upload、repo/user/role/task 管理 | ✅ | **P0** 骨架、逐階段補 |
| Search（by format / group / name / version / checksum / 自訂屬性） | ✅ | **P1** 起 |
| REST API（`/service/rest/v1`） | ✅ | ✅ 自有 `/api/v1`；Nexus 相容子集 **P7** |
| OpenAPI / Swagger UI | ✅ | **P0** |
| Webhooks（repository / audit / global events） | ✅ | **P5** |
| Email server 設定 | ✅ | **P4** |
| HTTP 設定（user agent、timeout、outbound proxy、non-proxy hosts） | ✅ | **P1** |
| System status / health check | ✅ | ✅ `/healthz` `/readyz` `/api/v1/status/check` |
| Health check 明細（`status/check`：CPU、blob store quota/ready、預設 admin 密碼未改警告、加密金鑰、scheduler、thread deadlock、NuGet V2 警告、pending upgrade） | ✅ | **P4** `/api/v1/status/check` 同樣逐項回報 |
| `http.forwarded` capability（信任 X-Forwarded-* header） | ✅ | **P0**（反向代理後面必備） |
| UI 設定：session timeout、request timeout、status 輪詢間隔、頁面標題 | ✅（`rapture.settings`） | **P5** |
| UI 上傳支援的格式清單（18 種：apt、maven2、raw、alpine、ansiblegalaxy、composer、conda、go、helm、npm、nuget、pypi、pub、r、rubygems、swift、terraform、yum） | ✅（`formats/upload-specs`） | 每個 plugin 宣告自己的 upload spec，UI 據此產生表單 |
| Blob store：softQuota、blobCount、totalSize、availableSpace 回報 | ✅ | **P4** |
| Node identity（多節點識別） | ✅ | 後（HA 時） |
| Metrics（Prometheus） | ✅（`/service/metrics/prometheus`） | ✅ `/metrics`（基本指標） |
| Logging：UI 看 log、動態調 log level | ✅ | **P4** |
| Support zip | ✅ | 後 |
| Logs：UI 直接看 log 檔內容 | ✅ | **P4** |
| System Information 頁（JVM／OS／環境變數／bundle 清單） | ✅ | **P4**（Go runtime、build info、環境） |
| Recovery Mode（安全模式修復資料一致性） | ✅（3.9x） | 後 |
| Data Store 設定頁（顯示 DB 連線） | ✅ | **P0**（唯讀顯示） |
| Proprietary Repositories（標記哪些 hosted 是自家元件，供 IQ namespace confusion 防護用） | ✅ | 不做（IQ 專用） |
| Nodes 頁（節點清單） | ✅ | 後（HA 時） |
| Licensing 頁 | ✅ | 不做（沒有 Pro 授權概念） |
| Nexus One UI 切換 | ✅（3.9x 新 UI） | 不做 |
| UI 搜尋列同時搜 component 與 CVE | ✅ | 只搜 component |
| Repositories 列表：Size、Health check、Firewall Report 欄位 | ✅ | Size 有；後兩者不做（IQ） |
| Capabilities 系統（功能開關） | ✅ | 用設定檔取代 |
| Scripting API（Groovy） | ✅（預設關） | 不做（用 REST + CLI 取代） |
| Backup / restore | ✅（H2 export） | **P7**（`holiao admin backup`） |
| Nexus 升級／migration 工具 | ✅ | ✅ `holiaokho import-nexus`（PostgreSQL + blob store、hardlink、idempotent） |
| Branding、Outreach、Analytics 上傳 | ✅ | 不做 |
| Malware remediation / Repository Firewall / RHC | IQ / Pro | 不做（可外掛整合 Trivy／Grype，後） |

## 7. Pro-only 功能（我們可以做為超越 CE 的賣點）

| 功能 | 規劃 |
|---|---|
| CE 上限：100,000 components、每日 200,000 requests | ✅ **無上限** |
| User tokens | **P5** |
| SAML | 後 |
| HA / clustering | 後（架構不擋，見 §1 非目標） |
| Staging / tagging / promotion | 後 |
| Repository replication | 後 |
| Group blob store | 後 |
| Docker group push | 後 |
| Content replication、Import/Export | 後 |

---

## 使用方式

- 新增功能前先對這張表；每完成一項改狀態
- 發現 Nexus 有而表上沒有的功能 → 先加進表，再決定階段
- 「不做」的項目要有理由，且要能被 AndyShiu 推翻
