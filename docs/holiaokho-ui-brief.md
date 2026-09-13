# Holiaokho（好料庫）Web UI — 設計 Brief

> 給 Claude Design 的完整說明：這個產品是什麼、給誰用、有哪些頁面、每頁的資料與操作、關鍵流程、風格方向、技術限制。
> 讀完這份文件應能直接產出設計系統與所有頁面的 layout，不需要再問後端問題。
> 後端 API 已完成，規格在 `/api/v1/openapi.yaml`（32 個 path）；本文件以使用者角度描述，不重複 API 細節。
> 2026-09-13。

---

## 1. 產品是什麼

**Holiaokho**（台語「好料庫」hó-liāu-khòo，英文 tagline："the good-stuff store"，發音 ho-LIAO-kho）是一套**自架的 artifact repository manager**：開發團隊把所有套件（Java、JavaScript、Docker image、Python、.NET、Go、Linux 套件…共 26 種格式）集中存放、快取、分發的地方。

它是 **Sonatype Nexus Repository 的直接替代品**：同樣的 URL 結構、同樣的 repo 概念，但是單一 Go binary、記憶體用量低、功能沒有 OSS 版的閹割（不限 component 數、不限每日請求數、有 OIDC、有 user token）。

一句話定位：**「開發團隊的內部套件倉庫，裝了就能無痛換掉 Nexus。」**

### 核心概念（UI 各處都會用到）

| 概念 | 說明 | UI 用語 |
|---|---|---|
| **Repository** | 一個有名字的套件倉庫，屬於一種 **format**（maven、npm、docker…）與一種 **type** | Repository |
| **type: hosted** | 團隊自己上傳的內容 | Hosted |
| **type: proxy** | 上游（Maven Central、npm registry、Docker Hub…）的快取，第一次下載後留在本地 | Proxy |
| **type: group** | 把多個 hosted + proxy 合成一個 URL，client 只要設一個位址 | Group |
| **Package** | 一個邏輯套件版本（Maven 的 groupId:artifactId:version、npm 的 name@version、Docker 的 image:tag） | Package（或依格式顯示 Component） |
| **Asset** | 實際檔案（.jar、.tgz、manifest、layer） | Asset / File |
| **Blob** | 內容位址儲存的實體 bytes，跨 repo 去重（使用者通常不需看到） | — |
| **Storage** | blob 存哪裡：本機磁碟或 S3 相容儲存 | Storage |
| **Format** | 套件生態系協定 | Format |

**26 種格式**：Maven、npm、Docker/OCI、PyPI、raw、NuGet、Helm、Go、APT、YUM、Alpine、RubyGems、Cargo、Composer、Conda、R、p2、CocoaPods、Terraform、pub（Dart）、Git LFS、Hugging Face、Ansible Galaxy、Conan、Swift。每種都有自己的 icon 需求（可用該生態系的慣用色／字母縮寫，不可直接用各生態系的商標 logo）。

---

## 2. 使用者

| 角色 | 是誰 | 使用頻率 | 主要任務 |
|---|---|---|---|
| **Admin / DevOps**（主要設計對象） | 架設與維護的人（1–3 人） | 建置期天天用，穩定後偶爾 | 建 repo、設權限、看健康狀態、清空間、處理 CI 抱怨 |
| **Developer**（次要） | 團隊工程師（10–100 人） | 偶爾 | 找套件、看版本、複製「怎麼設定 client」的指令、建自己的 token |
| **CI 機器人** | GitHub Actions / GitLab CI / Jenkins | 每天上千次 | **不用 UI**，只打 repo URL 與 API |
| **稽核 / 主管** | 偶爾看 | 很少 | 誰上傳了什麼、空間用量趨勢 |

設計重點：**Admin 的效率與可信賴感** 優先；Developer 的頁面要「不用登入也能瀏覽（若匿名開啟）、三秒找到套件、一鍵複製設定」。

---

## 3. 品牌與風格方向

- **名字來源**：台語「好料」= 東西齊全、有真材實料；「庫」= 倉庫。品牌要有**台灣／東亞的自信與親切**，但介面本身是**國際化的開發者工具**（五種語言）。
- **調性**：務實、快速、可信賴、有一點溫度。不是酷炫的消費產品，也不是冷冰冰的企業後台。
- **參考感覺**：Grafana 的資訊密度、Linear 的俐落、GitHub 的可讀性。**避免**看起來像 Nexus（深灰＋綠的 Sonatype 風格）或 JFrog（綠色）。
- **顏色**：未定，請提案。可從「好料」聯想（例：溫暖的橙／琥珀當強調色，搭配沉穩的深藍灰主色），但務必在 light／dark 兩種主題都成立，且與各格式 icon 的色彩不打架。
- **Logo / wordmark**：需要提案。文字 "Holiaokho"，可搭配一個代表「倉庫／格子／庫」的簡潔符號；中文「好料庫」作副標。首頁與 README 會用 `Holiaokho (hó-liāu-khòo, "the good-stuff store")` 的寫法。
- **字體**：介面用系統字（考慮 CJK：Noto Sans TC/SC/JP/KR 或系統預設），程式碼／路徑／指令一律等寬字。
- **元件庫限制**：前端決定用 **React + TypeScript + Ant Design**。設計請以 AntD 的元件語彙為基礎（Table、Form、Drawer、Modal、Tree、Tabs、Tag、Steps…），自訂 design tokens（色彩、圓角、間距、字級）即可，避免設計出 AntD 做不出來的互動。

---

## 4. 全站結構（Information Architecture）

```
/ui
├── 登入（local 帳密 / LDAP / 「用 SSO 登入」按鈕，依伺服器設定顯示）
├── Dashboard
├── Browse（瀏覽所有 repository 與內容）        ← Developer 主要入口
├── Search（跨 repo 搜尋套件）
├── Repositories（管理）
│   ├── 列表
│   ├── 建立精靈（選格式 → 選型態 → 設定）
│   └── 單一 repo：設定 / 內容 / 使用方式 / 統計
├── Storages
├── Security
│   ├── Users
│   ├── Roles（含 privileges 編輯）
│   ├── Content Selectors
│   ├── Auth Settings（realm 順序、LDAP、OIDC、Rut Auth、default roles、匿名）
│   └── 我的 Tokens（每個登入者都有，放在右上角個人選單也可）
├── Maintenance
│   ├── Tasks（排程與執行記錄）
│   ├── Cleanup Policies（含預覽）
│   ├── Routing Rules（含測試）
│   └── Backup / Restore
├── Integrations
│   ├── Webhooks
│   └── Email
├── System
│   ├── Health / Status
│   ├── System Information
│   ├── Logs（即時 tail + log level）
│   ├── Configuration（唯讀、遮蔽密碼）
│   ├── Support ZIP
│   └── Audit Log
└── Migrate from Nexus（說明頁 + CLI 指令；匯入本身走 CLI）
```

左側固定導覽（可收合成 icon）、頂部：全域搜尋、語言切換、主題切換、通知（任務失敗／配額警告）、個人選單（Tokens、改密碼、登出）。

**權限影響顯示**：匿名或一般 Developer 只看得到 Browse／Search／我的 Tokens；Admin 看全部。沒權限的項目**不顯示**而非 disabled。

---

## 5. 各頁面：資料、操作、狀態

以下每頁列出「看到什麼」「能做什麼」「空狀態／錯誤」。資料欄位名稱對應 API。

### 5.1 登入
- 顯示哪些登入方式由 `GET /api/v1/auth/methods` 決定：`local`（帳密）、`ldap`（同一個帳密表單，不需區分）、`oidc`（一顆「使用 SSO 登入」按鈕，跳轉）。
- 匿名開啟時，登入頁要有「先逛逛」的入口。
- 錯誤：帳密錯（401）、登入次數過多（429，顯示「請稍後再試」）。
- 首次登入若 admin 仍用預設密碼，登入後跳強制改密碼流程（health check 會回報 `default_admin_password`）。
- 所有設密碼的表單（建立使用者、重設、個人改密碼）要顯示目前政策的規則清單（來源 `GET /auth/methods` 的 `passwordPolicy`），輸入時即時打勾／打叉；送出後伺服器回 `password.*` code + params，由前端翻譯顯示。

### 5.2 Dashboard（Admin）
- **健康卡片**：DB、每個 storage（用量／配額，>90% 警示）、scheduler、預設密碼未改。來源 `GET /status/check`。
- **數字**：repositories 數、packages 數、blob 總大小、今日請求量（`/metrics`）。
- **最近活動**：audit log 最新 10 筆（誰、做了什麼、對哪個 repo）。
- **任務**：最近失敗的任務、下一次執行時間。
- **快速動作**：建立 repository、匯入 Nexus 說明。
- 空狀態（全新安裝）：引導三步驟——改密碼 → 建第一個 proxy repo → 複製 client 設定。

### 5.3 Browse（所有人）
- 左：repository 列表（可依 format／type 篩選，顯示 format icon、type 標籤、大小、線上／離線）。
- 右：選定 repo 後的**樹狀目錄**（`GET /repositories/{name}/browse?path=`），資料夾可展開；檔案列顯示大小、更新時間、sha256（可複製）、下載按鈕。
- Group repo 的瀏覽是合併後的結果；proxy 只顯示已快取的內容（要說明「這是快取，不代表上游全部」）。
- 檔案詳情 Drawer：路徑、大小、content-type、checksum、所屬 package、最後下載時間、下載 URL（可複製）、刪除（有權限時）。
- **使用方式 Tab**（很重要）：依 format 顯示可複製的 client 設定片段，例如 Maven 的 `settings.xml` mirror、npm 的 `.npmrc`、Docker 的 `docker login` 與 daemon `registry-mirrors`、pip 的 `--index-url`、Go 的 `GOPROXY`、Helm 的 `helm repo add`、APT 的 sources.list（含金鑰下載連結）…。URL 依當前 host 自動帶入。

### 5.4 Search（所有人）
- 一個搜尋框 + 篩選（format、repository、namespace/群組）；結果表：format icon、repository、namespace、name、version、最後下載、建立時間。
- 點擊進 package 詳情（`GET /packages/{id}`）：assets 清單、下載連結、刪除整個版本（Admin）。
- 空狀態：建議關鍵字格式（`gson`、`@babel/core`、`library/alpine`）。

### 5.5 Repositories（Admin）
**列表**：name、format、type、storage、online、packages、size、URL（可複製）、Docker 的 port／path 模式標示、routing rule、cleanup policies；列動作：編輯、內容、invalidate cache（proxy）、刪除（要輸入名稱確認）。

**建立精靈（3 步）**：
1. 選 **format**（26 個 icon 卡片，含搜尋）
2. 選 **type**（hosted／proxy／group，各附一句說明與適用情境）
3. **設定**：
   - 共同：name（規則：字母數字 `-_.`）、storage、online、routing rule、cleanup policies
   - proxy：remoteUrl（依 format 給預設值，例如 Maven Central、registry.npmjs.org、registry-1.docker.io）、contentMaxAge／metadataMaxAge（分鐘，-1 = 永久）、negative cache TTL、上游帳密、blocked／autoBlock
   - hosted：writePolicy（allow／allow_once／deny，附說明）
   - group：members 排序清單（拖曳排序，只列同 format）
   - format 專屬：Maven（layoutPolicy STRICT/PERMISSIVE、versionPolicy RELEASE/SNAPSHOT/MIXED）、Docker（httpPort、httpsPort＋憑證路徑、subdomain、forceBasicAuth、indexType HUB/REGISTRY、pathEnabled）、APT（distribution、component、簽章金鑰）、YUM／Alpine（簽章金鑰）、Cargo（downloadUrl 範本）…。簽章金鑰欄位旁有「產生金鑰」按鈕（`POST /system/pgp-key`、`/system/rsa-key`），並提供「下載公鑰」給 client 使用。
   - 建立後直接進「使用方式」頁。

**單一 repo 頁**：Tabs = 設定（同精靈第 3 步）／內容（同 Browse 該 repo）／使用方式／統計（packages、assets、size、命中率若有）。

### 5.6 Storages（Admin）
- 列表：name、type（fs／s3）、路徑或 bucket、used、blobs 數、quota、可用狀態。
- 建立：fs（path）或 s3（endpoint、region、bucket、prefix、accessKey、secretKey、pathStyle）；「測試連線」。
- 設定配額（bytes，UI 用 GB 輸入）。
- 說明區：「內容位址去重：同一個檔案在多個 repo 只存一份」。

### 5.7 Users / Roles / Content Selectors / Auth Settings（Admin）
- **Users**：username、display name、email、source（local／ldap／oidc／rut 標籤）、roles、active；建立（local 才能設密碼）、編輯 roles、停用、重設密碼、刪除。外部來源的使用者標示「由 LDAP/OIDC 同步」，roles 由 mapping 決定不可手改（或可加本地角色，需標示）。
- **Roles**：id、name、description、privileges 表格。privilege 編輯器：target 型別下拉（整個系統 `*`、某 repo、某 format 所有 repo、應用區域 `app:*`、content selector）＋ actions 多選（read／write／delete／admin）。內建角色（admin、anonymous、developer）標示不可刪。
- **Content Selectors**：name、expression（等寬字、語法提示：`format == "maven" and path =^ "/com/acme/"`）、「測試」面板（輸入 format＋path 看 match 與否）。
- **Auth Settings**：分區
  - Realms 順序（local、ldap 拖曳）與匿名存取開關（顯示目前匿名角色的權限摘要）
  - Default roles（多選）
  - Password policy（minLength、maxLength、需含大寫／小寫／數字／符號四個開關、不可包含帳號、拒絕常見密碼）
  - LDAP 表單（url、startTLS、bindDN、密碼、userBaseDN、userFilter、group 設定、roleMapping 表格、timeout）＋「測試連線／測試登入」
  - OIDC 表單（issuer、clientId、secret、scopes、usernameClaim、groupsClaim、roleMapping、redirectUrl 顯示供 IdP 註冊）
  - Rut Auth（header、trustedProxies CIDR 列表、autoCreate、defaultRoles）——要有明顯警語「只在反向代理後方啟用」

### 5.8 我的 Tokens（所有登入者）
- 列表：name、prefix、建立、最後使用、到期；建立（name、到期日）→ **一次性顯示 secret** 的 Modal（大字、複製按鈕、說明「關閉後不再顯示」）；撤銷。
- 說明區：token 可當密碼用於任何 client（Basic）、`Authorization: Bearer`、NuGet ApiKey、cargo token…。

### 5.9 Tasks（Admin）
- 列表：name、description、排程（interval 或 cron）、enabled、上次執行＋狀態、下次執行、執行中指示；動作：立即執行、設定 cron（cron 輸入 + 人類可讀說明 + 下次幾次執行預覽）、啟用/停用。
- 執行記錄：時間、狀態、log（等寬、可展開）。
- 內建任務：blob-gc、compact-blobs、cleanup-policies、prune-expired、rebuild-indexes、backup（若設定）。

### 5.10 Cleanup Policies（Admin）
- 列表：name、format、criteria 摘要、套用的 repos。
- 表單：lastDownloadedDays、lastUpdatedDays、keepLatest、nameRegex、versionRegex、prerelease；**預覽**：選一個 repo → 顯示「會刪除 N 個套件」與清單（分頁）。這是使用者最怕誤刪的地方，預覽要醒目。
- 指派到 repo（多選）。

### 5.11 Routing Rules（Admin）
- 列表：name、mode（allow／block）、matchers（regex 清單）、使用中的 repos；表單 + 「測試路徑」面板（`/routing-rules/test`）。

### 5.12 Backup / Restore（Admin）
- 顯示排程備份設定（目錄、含 blobs、保留數，來自設定檔，唯讀）、最近備份記錄（task runs）。
- 「立即下載備份」（可勾含 blobs，警告大小）。
- 「還原」：上傳 tar.gz，兩段確認（會覆蓋所有資料）。

### 5.13 Webhooks / Email（Admin）
- Webhooks：name、url、events（多選：`asset.*`、`repository.*`、`user.*`、`task.failed`、`*`…）、repository 篩選、secret、enabled；「送測試事件」；說明簽章 header。
- Email：host、port、username、password、from、startTLS／SSL、收件人清單；「寄測試信」。

### 5.14 System（Admin）
- **Health**：同 dashboard 卡片的完整版。
- **Information**：版本、Go、OS、CPU、記憶體、DB 版本與連線數、各表列數、storages、formats、相依套件清單（可折疊）。
- **Logs**：最近 N 行（等寬、可依 level 上色、自動更新開關、複製）、log level 下拉即時切換。
- **Configuration**：目前設定 YAML/JSON 唯讀顯示（密碼已遮蔽）。
- **Support ZIP**：一顆下載鈕與說明內容物。
- **Audit Log**：時間、actor、action、target、detail（JSON 可展開）、篩選與分頁。

### 5.15 Migrate from Nexus
- 靜態說明頁：三步驟（Nexus 還在跑時匯入 → 切換 port → 驗證），顯示可複製的 `holiaokho import-nexus --nexus-db ... --nexus-blobs ... --link` 指令，以及「換掉之後 CI 不用改設定」的對照表（URL 相同、Docker port 相同、帳密相同）。

---

## 6. 關鍵流程（要畫 flow / 逐頁 mockup）

1. **首次安裝**：登入（admin/預設密碼）→ 強制改密碼 → Dashboard 空狀態引導 → 建立第一個 proxy（例如 Maven Central）→ 「使用方式」複製 settings.xml。
2. **Developer 找套件**：匿名進 Browse → 搜尋 `gson` → 看到在哪個 repo、哪些版本 → 複製下載 URL 或 client 設定。
3. **CI token**：Developer 登入 → 我的 Tokens → 建立 → 複製 secret → 說明放進 CI secret。
4. **Admin 設 LDAP**：Auth Settings → 填 LDAP → 測試登入成功 → 設 roleMapping → 把 ldap 加進 realm 順序。
5. **空間不夠**：Dashboard 配額警告 → Cleanup Policies 建規則 → 預覽 → 指派 → 執行 → Tasks 看結果 → compact-blobs 回收。
6. **從 Nexus 搬家**：Migrate 頁照做 → Repositories 列表看到匯入的 repo → 驗證。

---

## 7. 多語系與在地化

- 語言：**en、zh-TW、zh-CN、ja、ko**，右上角切換，記住偏好；預設跟瀏覽器。
- 後端錯誤回 `{code, message, params}`，前端依 `code` 查翻譯（例：`repo.redeploy_denied` → 「此 repository 不允許重複部署同版本」）。設計時錯誤訊息預留**兩行**空間。
- 文字長度：日文／中文通常較短，英文／韓文較長；按鈕與表格欄寬要能容納 +30%。
- 日期時間：依語系格式，顯示相對時間（「3 分鐘前」）並 hover 顯示絕對時間。
- 檔案大小：KB/MB/GB 依語系。
- 不需要 RTL。

---

## 8. 版面與裝置

- **桌面優先**（Admin 都在桌機），寬度 1280–1920；資料表多、要善用寬度。
- 平板（768–1024）：導覽收合、表格可橫向捲動、表單單欄。
- 手機（<480）：只保證 Browse／Search／登入／我的 Tokens 可用；Admin 頁面可降級但不必精雕。
- **Light 與 Dark 兩套主題**皆為一等公民（開發者常用 dark）。
- 無障礙：對比 ≥ 4.5:1、鍵盤可操作所有表單與表格、焦點可見、icon 都有文字標籤。

---

## 9. 狀態與細節（每個列表／表單都要有）

- Loading（skeleton）、空狀態（附下一步動作）、錯誤（可重試、顯示錯誤碼）、權限不足（不顯示或友善說明）。
- 破壞性操作（刪 repo、刪 user、還原備份、執行 cleanup）：Modal 要求輸入名稱或勾選確認。
- 大量資料：repositories 可能 100+、packages 可能 100k+、assets 更多——表格要分頁／虛擬捲動、搜尋要防抖。
- 複製到剪貼簿的元件會非常多（URL、指令、checksum、token）：需要統一的「可複製文字」元件與成功提示。
- 即時性：Tasks 執行中、Logs tail、health 卡片要能輪詢更新（不需 websocket）。
- Docker 特殊：repo 有多種存取位址（port 模式 `host:5000/image`、path 模式 `host/repo/image`、subdomain），「使用方式」要清楚分開。

---

## 10. 技術限制（設計需知道）

- SPA 掛在 `/ui/`（其他路徑是套件 client 用的 `/repository/...`、`/v2/...`），build 後打包進 Go binary；沒有 SSR。
- 所有資料都來自 `/api/v1`（OpenAPI：`/api/v1/openapi.yaml`），無 GraphQL、無 websocket。
- 認證：session cookie（登入表單）或 Bearer token；OIDC 登入是整頁跳轉回 `/api/v1/auth/oidc/callback` 再導回 `next`。
- 匿名使用者可能有讀權限：UI 要能在未登入狀態渲染 Browse／Search。
- 圖示：各格式 icon 需自行設計（不可用第三方商標）；可用簡潔的字母／幾何圖形＋生態系慣用色（Java 橙、npm 紅、Docker 藍、Python 黃藍、Go 青、Rust 橙、Ruby 紅…）。

---

## 11. 期望交付

1. **設計系統**：色彩（含 light/dark）、字級、間距、圓角、陰影、狀態色；對應到 Ant Design token 名稱。
2. **Logo / wordmark** 提案 2–3 款與 favicon。
3. **頁面 layout**：第 4 節每一頁的桌面版（重點頁另附平板版）；第 6 節六個流程的逐頁 mockup。
4. **元件規範**：format icon 集（26 個）、type 標籤（hosted/proxy/group）、可複製文字、健康狀態卡片、privilege 編輯器、cron 編輯器、一次性 secret Modal、預覽刪除清單。
5. **空狀態／錯誤／loading** 的通用樣式。
6. 一份「設計說明」讓前端工程師照做（含 i18n 與 dark mode 注意事項）。

---

## 附錄 A：對照 Nexus 的 UI（作為「不要長這樣」的參考）

Nexus 3 的 UI 是左側設定樹（Repository／Security／Support／System）＋右側 ExtJS 表格，密度高但老舊、對開發者不友善（沒有「怎麼用」的引導、搜尋分散）。我們保留它「管理項目齊全」的優點，改善：Browse 與 Search 給非管理者的體驗、每個 repo 的使用方式片段、預覽式的清理、現代的 dark mode 與多語系。

## 附錄 B：名詞翻譯建議（zh-TW）

Repository → 儲存庫（或直接用 Repository）、Hosted → 自有、Proxy → 代理快取、Group → 群組、Package → 套件、Asset → 檔案、Storage → 儲存空間、Cleanup Policy → 清理規則、Routing Rule → 路由規則、Content Selector → 內容選擇器、Token → 存取權杖、Realm → 認證來源、Audit Log → 稽核記錄、Blob → 內容區塊。其他語言由翻譯階段處理，但設計稿可用 en 與 zh-TW 兩種文案示範。
