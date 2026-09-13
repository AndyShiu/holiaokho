# Holiaokho（好料庫）Web UI — 設計 Brief

> 給 Claude Design 的完整說明：這個產品是什麼、給誰用、有哪些頁面、每頁的資料與操作、關鍵流程、風格方向、技術限制。
> 讀完這份文件應能直接產出設計系統與所有頁面的 layout，不需要再問後端問題。
> 第 5 節是全站共用的業務邏輯，第 6 節逐頁列出元件、互動與前端業務邏輯（欄位名皆對應實際 API）。
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

## 5. 全站共用的業務邏輯（每頁都依賴）

設計與前端都要先理解這一節，後面每頁只寫該頁特有的部分。

### 5.0.1 啟動與身分
1. App 載入先打 `GET /api/v1/auth/methods`（公開）→ 得到 `{local, ldap, oidc, oidcLoginUrl, anonymous, passwordPolicy}`，存成全域 `authMethods`。
2. 再打 `GET /api/v1/session`（whoami）→ `{username, roles[], anonymous, via, privileges[]}`。
   - `anonymous: true` 且 `authMethods.anonymous: true` → 以匿名身分渲染（可看 Browse／Search）。
   - `anonymous: true` 且 `authMethods.anonymous: false` → 導向登入頁，並記住原本路徑做 `next`。
   - 401 → 同上。
3. `privileges[]` 是 `{target, actions[]}`；前端用它決定**導覽顯示與按鈕出現**（不是 disabled，是不出現）。判斷函式：
   - `can(target, action)`：target 精確相符、或 privilege target 為 `*`、或同前綴的萬用（`app:*`、`repo:*`、`format:*`）；actions 含該 action 或 `*`。
   - 導覽對照：Repositories 管理 → `app:repositories read`；Storages → `app:storages read`；Users → `app:users read`；Roles／Content Selectors → `app:roles read`；Tasks → `app:tasks read`；Auth Settings／Webhooks／Email／System／Audit → `app:system read`；Search → `app:search read`；Dashboard 健康卡片 → `app:status read`。
   - repo 層級：上傳／刪除檔案按鈕 → `repo:<name> write/delete`（或 `format:<format>`、`*`）。
4. 登入後把 whoami 重新拉一次；登出 `DELETE /session` 後清空並回登入頁。
5. Session 逾時：任何 API 回 401 → 全域攔截，彈「登入已逾時」→ 導向登入並帶 `next`；**不要**在匿名可讀的頁面上彈（匿名 401 只代表這個操作要登入，改顯示「登入以繼續」按鈕）。

### 5.0.2 錯誤處理
- 後端錯誤格式固定 `{code, message, params?}`，HTTP 狀態：400 驗證、401 未登入、403 無權限、404、409 衝突（名稱重複、版本已存在）、429 限流、502 上游失敗、507 配額。
- 前端翻譯順序：`errors.<code>`（帶 params）→ 沒有翻譯就顯示 `message` 原文，並在旁邊小字顯示 code（方便回報）。
- 表單類錯誤（400／409）顯示在表單頂端的 Alert 並保留使用者輸入；列表操作類錯誤用 toast。
- 502 upstream 類錯誤附「查看上游設定」連結（proxy repo）。

### 5.0.3 列表通用行為
- 後端目前**回整包陣列**（repositories、users、roles、storages、tasks、policies…都不分頁）；只有 `search`、`packages`、`audit`、`logs` 有 `limit/offset` 或 `tail`。前端對整包陣列做本地排序／篩選／分頁（AntD Table 內建）；search 與 packages 用伺服器分頁（每頁 50，`offset` 累加）。
- 每個列表都有：頂部工具列（搜尋框 debounce 300ms、篩選、主要動作按鈕靠右）、欄位可排序、列點擊進詳情、列尾 `⋯` 動作選單。
- 刪除一律走確認 Modal；「高風險」（repo、storage、user、還原備份、cleanup 執行）要求**輸入名稱**才能按確認。

### 5.0.4 表單通用行為
- 用 Drawer（右側，寬 560–720）做建立／編輯，列表保持在後面；多步驟精靈（建 repo）用整頁。
- 髒表單離開時提示。
- 儲存成功：toast + 關閉 Drawer + 重新拉列表；失敗：Alert 留在 Drawer 內。
- 秘密欄位（密碼、secretKey、token secret、簽章私鑰）：後端讀回一律是 `***`。前端規則：**欄位留空或維持 `***` = 不變更**；使用者輸入新值才送出。UI 用 password input + 「已設定，留空保留」的 placeholder。

### 5.0.5 時間、大小、複製
- 時間欄顯示相對時間（`3 分鐘前`），hover 顯示 ISO 絕對時間，皆依語系。
- bytes 顯示成 KB/MB/GB（1024 進位）；配額輸入用 GB，送出時 ×1024³ 轉 bytes。
- `<Copyable>` 元件：等寬字、尾端複製 icon、點擊後 icon 變勾勾 1.5 秒；長字串（sha256、token）中間省略、hover 顯示全文。
- 「使用方式」程式碼片段：語法高亮、右上角複製、內容用 `window.location.origin` 帶入 base URL（若後端 `status.baseUrl` 有設則優先）。

### 5.0.6 輪詢
- 不用 websocket。Dashboard 健康卡片每 30s、Tasks 列表在有 `running: true` 時每 3s、Logs 開啟自動更新時每 2s、其他頁不輪詢。頁面不可見（`document.hidden`）時暫停。

### 5.0.7 多語系
- 語言存在 localStorage，初值取瀏覽器語言對應（zh-TW／zh-CN／ja／ko／en，其他 → en）。
- 所有文案 key 化；後端 code 對照表放在 `errors.*`；format 名稱、type 名稱、task 名稱也要有翻譯。

---

## 6. 各頁面詳細規格

每頁格式：**路由／權限** → **版面與元件** → **資料來源** → **互動** → **業務邏輯** → **狀態**。

### 6.1 登入 `/ui/login`

**權限**：公開。已登入者進來直接導向 `next` 或 Dashboard。

**版面與元件**
- 置中卡片（寬 400）：Logo + wordmark、副標「好料庫」。
- 表單：username、password（可顯示／隱藏）、「登入」按鈕（loading 狀態）。
- `authMethods.oidc` 為 true → 表單下方分隔線＋「使用 SSO 登入」按鈕。
- `authMethods.anonymous` 為 true → 底部連結「先瀏覽套件（不登入）」→ Browse。
- 語言切換放右上角（登入前就要能切）。

**資料來源**：`POST /api/v1/session {username, password}` → 200 設 cookie；`GET /api/v1/auth/oidc/login?next=<path>` 整頁跳轉。

**互動與業務邏輯**
- Enter 送出；送出中禁用表單。
- 401 → 表單頂端 Alert「帳號或密碼錯誤」（不區分哪個錯）；429 → Alert「登入失敗次數過多，請稍後再試」並把按鈕鎖 30 秒倒數。
- 成功後：重拉 whoami → 若 `can("app:status","read")` 則額外打 `GET /status/check`，`checks.default_admin_password.healthy === false` → 導向**強制改密碼**頁（6.2），否則導向 `next`（只接受站內相對路徑，防 open redirect）或 Dashboard。
- OIDC 回來時 callback 已由後端處理並導回 `next`；前端只需在載入時重拉 whoami。
- 一律不記住密碼、不提供「忘記密碼」（管理員重設）。

**狀態**：無空狀態；後端不可達顯示「無法連線到伺服器」與重試。

### 6.2 強制改密碼（首次登入）`/ui/change-password?forced=1`

**版面**：全頁置中卡片、不顯示側欄。標題「請先更換預設密碼」，說明一句。
- 欄位：目前密碼、新密碼、確認新密碼。
- **密碼規則清單元件 `<PasswordRules>`**：依 `authMethods.passwordPolicy` 動態列出規則（最少 N 碼、含大寫、含小寫、含數字、含符號（若要求）、不含帳號、非常見密碼），使用者輸入時逐條即時打勾／打叉；「非常見密碼」這條前端無法判斷，顯示為中性 icon 並註明「送出時檢查」。
- 送出按鈕在「前端可判斷的規則全部通過且兩次輸入相同」前 disabled。

**資料來源**：`PUT /api/v1/me/password {current, password}` → 204。

**業務邏輯**
- 401（目前密碼錯）→ 目前密碼欄位錯誤；400 `password.*` → 對應規則列變紅並顯示訊息（例如 `password.common`）。
- 成功後重拉 whoami，導向 Dashboard；同一個元件不帶 `forced` 也用於個人選單的「改密碼」（放在 Drawer 裡）。

### 6.3 Dashboard `/ui/`

**權限**：登入者；卡片依權限顯示（無 `app:status` 就不顯示健康卡片；Developer 看到的是精簡版：我的 tokens 數、最近瀏覽的 repo、快速搜尋）。

**版面與元件（Admin）**
1. 第一列 **健康卡片群**：每個 check 一張小卡（icon＋名稱＋狀態色）：`database`、`storage:<name>`（顯示 usedBytes/quotaBytes 進度條，>90% 黃、超過紅）、`scheduler`、`default_admin_password`（不健康時整張紅並有「立即更改」按鈕）。頂端一顆總狀態 Badge：`healthy` 綠／有問題紅。
2. 第二列 **統計數字卡**：Repositories 數（依 type 拆 hosted/proxy/group）、Packages 總數、Assets 總數、總大小（sum of repo `stats.size`）。
3. 第三列左 **最近活動**（audit 最新 10 筆：相對時間、actor、動作翻譯、target 連結）；右 **任務**（`lastStatus === "failed"` 的任務列在最上、標紅；其餘顯示 nextRun；每列「立即執行」）。
4. 頂部右側 **快速動作**：「建立 Repository」「從 Nexus 搬家」。

**資料來源**：`GET /status/check`（30s 輪詢）、`GET /repositories`（含 stats）、`GET /audit?limit=10`、`GET /tasks`。

**業務邏輯**
- 全新安裝判斷：`repositories.length === 0` → 用**引導面板**取代統計卡：三步驟 Steps（改密碼 ✓ 依 `default_admin_password`、建立第一個 proxy → 開精靈並預選 maven/proxy、複製 client 設定 → 進該 repo 的使用方式）。
- 任何卡片載入失敗只影響該卡片（顯示重試），不整頁錯誤。

### 6.4 Browse `/ui/browse/:repo?/:path*`

**權限**：`app:repositories read` 不必；列表用 `GET /repositories`（後端依 repo 讀權限過濾）。匿名可用。

**版面與元件**
- 左欄（寬 280，可拖曳調整）：**Repository 清單**
  - 頂部：搜尋框（本地過濾 name）、篩選 chips（format 多選、type 多選）。
  - 每列：format icon、name、type Tag（Hosted 藍／Proxy 紫／Group 綠，顏色待設計）、右側灰字大小；`online: false` 顯示灰色與「離線」小 Tag。
  - 選中列高亮，URL 同步 `/ui/browse/<name>`。
- 右欄：
  - **標題列**：repo 名、format／type Tag、可複製的 repo URL（`url` 欄位）、右側按鈕群：「使用方式」（開 Drawer）、「上傳」（hosted 且有 write 權）、「重新整理」。
  - **Tabs**：`內容`、`套件`。
  - **內容 Tab**：麵包屑（可點回上層）＋**檔案表**：名稱（資料夾 icon 可點進入；檔案 icon）、大小、更新時間、`cacheExpiresAt`（proxy 才有，顯示「快取到期」相對時間；已過期灰字）、動作（下載、詳情、刪除）。資料夾在前、檔案在後，各自按名稱排序。
  - **套件 Tab**：表格 namespace／name／version／最後下載／建立時間，伺服器分頁；頂部有 name／namespace 篩選框；點列開 Package Drawer（見 6.5）。
  - proxy repo 內容表頂端一條淡色提示：「這裡只顯示已被快取的內容，不代表上游全部」。
  - group repo 提示：「合併自 N 個成員：a、b、c」，成員可點跳轉。

**檔案詳情 Drawer**（`GET /assets/{id}`）：路徑（Copyable）、大小、contentType、`blobDigest`（Copyable）、所屬 package（連結）、建立／更新／最後下載、快取到期、`negative: true` 時顯示「負快取（上游 404 記錄）」標籤、下載 URL（Copyable，`<repoUrl>/<path>`）、「刪除」按鈕（`repo:<name> delete`）。

**上傳 Drawer**（hosted only；`POST /repositories/{name}/upload` multipart `file`、`path`、`overwrite`）：拖放區、path 輸入（預設為目前瀏覽目錄＋檔名，可改）、overwrite 勾選（writePolicy 為 `allow_once` 時顯示說明「此 repo 不允許覆蓋，勾選也無效」）、進度條。409 `repo.redeploy_denied` → 提示版本已存在。

**資料來源**：`GET /repositories`、`GET /repositories/{name}/browse?path=`、`GET /repositories/{name}/packages?q&name&namespace&limit&offset`、`GET /assets/{id}`、`DELETE /assets/{id}`。

**業務邏輯**
- URL 是唯一狀態來源：`/ui/browse/maven-central/com/google/gson` 重新整理要能回到同位置。
- 進入資料夾時先顯示 skeleton，回應後替換；回上層用快取（保留最近 20 個目錄）。
- 「下載」直接開 `<repoUrl>/<path>`（瀏覽器 GET，靠 cookie 或匿名）。
- 刪除後從表格移除該列並 toast；若刪的是 package 最後一個 asset，後端會一併刪 package，套件 Tab 需重拉。
- Docker repo 的內容路徑是 `v2/<image>/manifests/<tag>` 與 `blobs/sha256:…`，前端在 Docker 格式下把 `manifests` 目錄的檔案顯示為「tag」、`blobs` 顯示 digest 短碼；套件 Tab 對 Docker 顯示 image:tag。

**狀態**：無 repo → 空狀態「還沒有 repository」（Admin 顯示建立按鈕）；repo 空 → 「此 repository 尚無內容」（proxy 附「第一次下載後就會出現」、hosted 附上傳按鈕）；離線 repo → 內容照常可瀏覽但標題列黃色 Alert「離線：client 請求會被拒絕」。

### 6.5 Search `/ui/search?q=&format=&repository=&namespace=`

**權限**：`app:search read`（匿名角色預設有）。

**版面與元件**
- 頂部大搜尋框（autofocus，Enter 或 debounce 400ms 觸發）＋篩選列：format（Select，附 icon）、repository（Select，依 format 連動）、namespace（Input）、version（Input）。
- 結果表：format icon、repository（連結到 Browse）、namespace、name、version、最後下載、建立時間；每頁 50，底部「載入更多」（offset 累加）或分頁。
- 列點擊 → **Package Drawer**：標題 `namespace/name@version`、所屬 repo、屬性（`attrs` 依 format 顯示：npm 的 description/license、maven 的 packaging、docker 的 digest/size…，其餘用 key-value 表）、**Assets 表**（path、大小、sha256、下載）、右上角「刪除此版本」（`repo:<name> delete`，確認 Modal）。
- 全域頂欄的搜尋框：輸入後 Enter 直接跳到本頁帶 `q`。

**資料來源**：`GET /search?q&format&repository&namespace&name&version&limit&offset`、`GET /packages/{id}`、`GET /packages/{id}/assets`、`DELETE /packages/{id}`。

**業務邏輯**
- `q` 對 name／namespace 做子字串比對（後端）；輸入含 `@`（npm scope）或 `:`（maven gav）時前端不特別解析，直接送。
- 篩選變更即重查並重設 offset；查詢參數全部同步到 URL。
- 沒有任何條件時不查，顯示空狀態與範例關鍵字（`gson`、`@babel/core`、`library/alpine`）。
- 刪除版本後從結果移除。

### 6.6 Repositories 管理 `/ui/admin/repositories`

**權限**：`app:repositories read`；建立／編輯 `write`；刪除 `delete`。

#### 列表
- 工具列：搜尋、format／type 篩選、「建立 Repository」主按鈕。
- 欄位：name（連結）、format icon＋名、type Tag、storage、online（Switch，直接切換 → `PUT /repositories/{name} {online}`）、packages、size、URL（Copyable，Docker 另顯示 `:httpPort`／path／subdomain 小 Tag）、routing rule 名、cleanup policies 數。
- 列動作：編輯（Drawer）、瀏覽內容（跳 Browse）、使用方式、invalidate cache（proxy／group only，`POST /{name}/invalidate-cache`，確認後 toast）、刪除（輸入名稱確認；`DELETE`；提示「內容與 blob 會在下次 blob-gc 回收」）。
- group 列 hover 顯示成員；proxy 列 `attributes.proxy.blocked` 顯示紅色「已封鎖」Tag；autoBlock 觸發中（後端暫時封鎖）目前無 API 顯示，略。

#### 建立精靈 `/ui/admin/repositories/new`（整頁 Steps）
**Step 1 選格式**：26 張卡片（icon、名稱、一句話），頂部搜尋。點選即進下一步。
**Step 2 選型態**：三張大卡 Hosted／Proxy／Group，各附說明與適用情境；Group 卡在該 format 沒有任何 repo 時 disabled 並註明「先建立 hosted 或 proxy」。
**Step 3 設定**（單一表單，分區塊）：
- **基本**：name（regex `^[A-Za-z0-9._-]+$`，即時驗證，409 → 「名稱已存在」）、storage（Select，`GET /storages`，預設 `default`）、online（預設 on）。
- **Proxy**（type=proxy）：remoteUrl（依 format 預填：maven→`https://repo1.maven.org/maven2/`、npm→`https://registry.npmjs.org/`、docker→`https://registry-1.docker.io`、pypi→`https://pypi.org/`、go→`https://proxy.golang.org`、helm 需使用者填、其他依表）、contentMaxAge（分鐘，預設 1440；`-1` 有 checkbox「永久快取」）、metadataMaxAge（預設 1440）、negativeCacheTtl（預設 1440；0 = 關閉）、上游帳密（username／password，密碼規則同 5.0.4）、blocked、autoBlock（預設 on）。
- **Hosted**（type=hosted）：writePolicy Radio：`allow`（可覆蓋）／`allow_once`（預設；同路徑不可重新上傳，Maven SNAPSHOT 例外）／`deny`（唯讀）。
- **Group**（type=group）：members 雙欄 Transfer 或可拖曳排序清單，只列同 format 的非 group repo；順序即解析順序，要有說明「先命中的成員優先」。
- **格式專屬**（依 Step 1 顯示）：
  - maven：layoutPolicy（STRICT／PERMISSIVE）、versionPolicy（RELEASE／SNAPSHOT／MIXED；hosted 才有意義）。
  - docker：httpPort（0=不開）、httpsPort＋tlsCert／tlsKey（PEM 路徑，伺服器端檔案）、subdomain、pathEnabled（預設 on）、forceBasicAuth、indexType（HUB／REGISTRY／CUSTOM；proxy 才顯示）。三種存取方式旁邊各有一行說明對應的 URL 長什麼樣。
  - apt：distribution（預設 `stable`）、component（`main`）、signingKey（PGP 私鑰 ASCII-armored textarea）＋passphrase、flat（proxy）。
  - yum：repodataDepth（0–5）、signingKey＋passphrase。
  - alpine：signingKey（RSA 私鑰 PEM）、keyName（預設 `holiaokho.rsa.pub`）。
  - cargo：downloadUrl 範本（proxy 可留空）。
  - 其他 format 無專屬欄位。
- **簽章金鑰產生器**：signingKey 欄旁「產生金鑰」→ Modal 輸入 name／email → `POST /system/pgp-key`（apt/yum）或 `POST /system/rsa-key`（alpine）→ 回 `{privateKey, publicKey}`：私鑰自動填入欄位，公鑰顯示在 Modal 內供下載／複製，並提醒「公鑰之後可在 `<repoUrl>/repository-key.gpg`（alpine：`<repoUrl>/<keyName>`）取得」。
- **Routing rule**（Select，可空）、**Cleanup policies**（多選；建立後逐一 `PUT /cleanup-policies/{id}/repositories/{name}`）。
- 底部：「建立」→ `POST /repositories` → 成功導向該 repo 詳情的「使用方式」Tab。

**業務邏輯**
- 送出 payload：`{name, format, type, storage, online, attributes: {proxy?|hosted?|group?, <format>?}}`；未顯示的區塊不送。
- 400 `validation` 顯示在對應欄位（後端 message 含欄位名時前端做對照，否則顯示在頂部）。
- 精靈可用「上一步」回去改 format／type，已填的共通欄位保留、格式專屬欄位清空。

#### 單一 repo 頁 `/ui/admin/repositories/:name`
Tabs：
- **設定**：同 Step 3 表單（name／format／type 唯讀灰顯）；儲存 `PUT /repositories/{name}`（只送 `online` 與 `attributes`）；proxy 的 password 顯示 `***`。
- **內容**：內嵌 Browse 右欄。
- **使用方式**：見 6.7。
- **統計**：packages、assets、size 三個數字＋（未來）下載次數；「Invalidate cache」與「刪除」放在此 Tab 底部危險區。

### 6.7 「使用方式」Drawer／Tab（`<UsageSnippets format type url>`）

純前端元件，無 API。依 format 產生 1–3 個片段，每個片段有標題、說明一句、程式碼區（可複製）。URL 用 repo `url`；Docker 依 `attributes.docker` 產生不同位址：
- port 模式：`<host>:<httpPort>/<image>`；path 模式：`<host>/<repo>/<image>`（`pathEnabled`）；subdomain：`<subdomain>.<host>/<image>`；proxy 另附 daemon `registry-mirrors` 設定。
- Maven：`settings.xml` 的 `<mirror>`（group／proxy）或 `<distributionManagement>`（hosted）＋`<server>` 憑證用 token。
- npm：`.npmrc` 的 `registry=` 與 `//host/repository/<name>/:_authToken=`；scope 版本。
- PyPI：`pip.conf index-url`、`twine upload --repository-url`。
- Go：`GOPROXY=<url>`（附 `GONOSUMDB`／`GOSUMDB=sum.golang.org <url>/sumdb` 說明）。
- Helm：`helm repo add`；hosted 附 `helm push`／curl upload。
- APT：`sources.list` 一行＋ `curl <url>/repository-key.gpg | gpg --dearmor`；YUM：`.repo` 檔含 `gpgkey=`；Alpine：`/etc/apk/repositories` ＋ 下載公鑰到 `/etc/apk/keys/`。
- NuGet：`dotnet nuget add source`、`nuget push -ApiKey <token>`。
- Cargo：`.cargo/config.toml` `[registries]` sparse；`cargo login`。
- Composer：`composer config repositories.holiao composer <url>`。Conda：`channels`。CRAN：`options(repos=)`。RubyGems：`gem sources -a`／`gem push --host`。pub：`PUB_HOSTED_URL`。Terraform：`provider_installation network_mirror`／module source。Git LFS：`.lfsconfig`。Hugging Face：`HF_ENDPOINT`。Ansible：`ansible.cfg server_list`。Conan：`conan remote add`。Swift：`swift package-registry set`。CocoaPods：`source`。p2：Eclipse update site URL。raw：`curl` 上傳／下載範例。
- 每個片段底部一行「憑證：使用 User Token（帳號 + token 當密碼）」連到我的 Tokens。
- 所有片段都有 **語言無關** 的程式碼，說明文字走 i18n。

### 6.8 Storages `/ui/admin/storages`

**權限**：`app:storages read/write`。

**版面**：卡片或表格：name、type Tag（fs／s3）、位置（fs 顯示 path；s3 顯示 `bucket/prefix @ endpoint`）、使用量進度條（`/status/check` 的 `storage:<name>` usedBytes／quotaBytes）、blobs 數（若有）、狀態（健康／錯誤訊息）。「建立 Storage」按鈕。

**建立 Drawer**（`POST /storages`）：name、type Radio；fs：path（伺服器端絕對路徑，提示「容器內路徑」）；s3：endpoint、region、bucket、prefix、accessKey、secretKey、pathStyle（MinIO／自架必開）。「測試連線」按鈕（目前沒有專用 API：前端先送建立，失敗即顯示錯誤；設計上保留按鈕位置，未來補 API）。

**編輯 Drawer**（`PUT /storages/{name}`）：只能改 quota（GB 輸入，0 = 無限制）與 s3 憑證；path／bucket 不可改（說明「改位置請建立新 storage」）。

**業務邏輯**
- `default` storage 不能刪（後端無刪除 API；UI 不提供刪除）。
- 配額滿時 client 上傳會收到 507；UI 在 storage 卡片與 Dashboard 都要紅色提示。
- 說明區塊（可收合）：「同一份檔案在多個 repo 只存一份（content-addressed）；刪除的內容由 blob-gc 任務回收，compact-blobs 才真正釋放磁碟」。

### 6.9 Users `/ui/admin/users`

**權限**：`app:users`。

**列表**：username、displayName、email、source Tag（local／ldap／oidc／rut）、roles Tags、active（Switch → `PUT /users/{u} {active}`）、createdAt；工具列搜尋、source 篩選、「建立使用者」。列動作：編輯、重設密碼（local only）、Tokens、刪除（輸入名稱；不可刪 `admin` 與自己、`anonymous` 不列出或列出但不可編輯密碼）。

**建立 Drawer**（`POST /users`）：username（regex 同 repo 名）、displayName、email、password＋確認＋`<PasswordRules>`、roles（多選，`GET /roles`）、active。
**編輯 Drawer**（`PUT /users/{u}`）：同上但無密碼；`source !== "local"` 時顯示 Alert「由 LDAP／OIDC 同步，roles 由 mapping 決定；此處加的角色為附加本地角色」。
**重設密碼 Modal**（`PUT /users/{u}/password`）：新密碼＋`<PasswordRules>`（username 帶入以檢查「不含帳號」）。
**Tokens Drawer**：同 6.12，但用 `/users/{u}/tokens`。

**業務邏輯**
- 409 `user.exists` → username 欄位錯誤。
- 自己的 roles 若移除 admin 會被鎖在外面：前端在把自己的 `admin` 角色拿掉時彈確認警告。
- `admin` 使用者不可停用、不可刪。

### 6.10 Roles `/ui/admin/roles`

**列表**：id、name、description、privileges 數、內建標記（`admin`、`anonymous`、`developer` 顯示「內建」Tag，不可刪，可編輯 privileges 但 `admin` 不可）。

**建立／編輯 Drawer**（`POST /roles`、`PUT /roles/{id}`）：id（建立時可填，regex 同上，留空自動）、name、description、**Privilege 編輯器**：
- 表格每列 = 一條 privilege：`target` ＋ `actions`。
- target 用兩段式輸入：**類型 Select**（整個系統 `*` ／ 應用區域 `app:` ／ 某 repository `repo:` ／ 某格式所有 repo `format:` ／ 內容選擇器 `selector:`）＋ **值**：app 用 Select（repositories、storages、users、roles、tasks、system、search、status、`*`）；repo 用 Select（含 `*`）；format 用 Select；selector 用「選擇器名 @ repo 或 *」兩個 Select。
- actions 用 Checkbox 群：read／write／delete／admin，或 `*`（勾 `*` 時其他 disabled）。
- 底部即時顯示「這個角色可以做什麼」的人話摘要（例如「讀取所有 repository；管理 users」）。
- 新增列、刪除列、複製列。

**業務邏輯**
- 至少一條 privilege 才能儲存。
- `selector:` target 只有在有 content selector 時可選，否則顯示「先建立 Content Selector」連結。

### 6.11 Content Selectors `/ui/admin/content-selectors`

**列表**：name、expression（等寬字截斷）、description、被哪些 roles 引用（前端從 roles 列表比對 `selector:<name>@`）。

**Drawer**（`POST`／`PUT /content-selectors`）：name、description、expression（CodeMirror 風格單行／多行、語法說明面板：支援 `format == "maven"`、`path =^ "/com/acme/"`（前綴）、`path =~ "regex"`、`and`／`or`／`not`／括號）。
**測試面板**（在 Drawer 內）：輸入 format＋path → `POST /content-selectors/test {expression, format, path, coordinate?}` → `{matches}` → 綠勾「符合」／灰叉「不符合」；表達式語法錯誤 400 顯示在 expression 欄下。

**業務邏輯**：被 role 引用中的 selector 刪除時警告會影響哪些 roles。

### 6.12 我的 Tokens `/ui/me/tokens`（個人選單）

**權限**：任何登入者（匿名不顯示）。

**列表**：name、prefix（`hlk_xxxx…`）、createdAt、lastUsedAt、expiresAt（過期列灰色＋「已過期」）；「建立 Token」；列動作「撤銷」（確認）。

**建立 Modal**（`POST /me/tokens {name, expiresAt?}`）：name（必填）、到期（Radio：30 天／90 天／1 年／永不／自訂日期）。成功後切換成 **一次性顯示畫面**：大字等寬 secret、複製按鈕、紅字「關閉後無法再看到，請立即保存」、勾選「我已保存」才能關閉。

**說明區**：token 當密碼用在任何 client（Basic）、`Authorization: Bearer <token>`、NuGet ApiKey、cargo token、npm `_authToken`；各附一行範例。

**業務邏輯**：secret 只存在該 Modal 的 state，關閉即清除；撤銷後立即從列表移除。

### 6.13 Auth Settings `/ui/admin/auth`

**權限**：`app:system`。單一頁、左側錨點導覽、右側分區表單，**每區獨立儲存**（都打同一支 `PUT /auth/settings`，前端合併目前設定＋該區變更後整包送出；秘密欄位維持 `***`）。

**區塊**
1. **Realms 與匿名**：Realms 可拖曳排序清單（local、ldap），未啟用的 realm 可移除；匿名存取狀態（唯讀顯示，來自設定檔 `auth.anonymous_enabled`，附「在設定檔或環境變數修改」說明）＋目前 `anonymous` 角色權限摘要（連到 Roles）。
2. **Default roles**：多選。
3. **Password policy**：minLength（≥8）、maxLength、四個 require 開關、disallowUsername、disallowCommon；右側即時範例「合格密碼長這樣」。
4. **LDAP**：enabled 開關；url（`ldap://`／`ldaps://`）、startTLS、bindDN、bindPassword、userBaseDN、userFilter（預設 `(uid={username})`）、userSubtree、emailAttr、displayNameAttr、group 區（groupBaseDN、groupFilter、groupNameAttr、memberAttr）、roleMapping 表（LDAP group → role 多選）、timeout。按鈕：「測試連線」「測試登入」（`POST /auth/ldap/test {config: <目前表單的 LDAP 區塊>, username, password}` → 用未儲存的設定直接測，顯示找到的 DN、email、groups、對應到的 roles）。
5. **OIDC**：enabled；issuer、clientId、clientSecret、scopes（tags）、usernameClaim、groupsClaim、roleMapping 表、autoCreate；唯讀顯示 redirect URL `<origin>/api/v1/auth/oidc/callback`（Copyable）供 IdP 註冊。
6. **Rut Auth**：enabled；header 名、autoCreate、defaultRoles；紅色 Alert「只有在反向代理會剝除此 header 且 `server.trusted_proxies` 已設定時才可啟用」，並唯讀顯示目前 trusted_proxies（來自 `/system/config`）。

**業務邏輯**
- 儲存 LDAP／OIDC enabled 但必填欄位空 → 前端擋。
- 把 `ldap` 從 realms 移除但 LDAP enabled → 提示「LDAP 已設定但不在 realm 順序中，不會被使用」。
- 修改後 30 秒內後端快取才更新（後端 settings cache 30s）；UI 儲存成功 toast 加註「最多 30 秒生效」。

### 6.14 Tasks `/ui/admin/tasks`

**列表**（`GET /tasks`，有 running 時 3s 輪詢）：name（翻譯＋原名）、description、排程（`cron` 有值顯示 cron＋人話；否則顯示 `interval`）、enabled Switch、lastRun（相對時間＋`lastStatus` 色點）、nextRun、running spinner；列動作：「立即執行」（`POST /tasks/{name}/run` → 202 → toast 並開始輪詢）、「設定排程」。

**排程 Modal**（`PUT /tasks/{name}/schedule {cron?, enabled}`）：Radio「預設間隔」／「Cron」；cron 輸入框＋常用範本（每天 02:00、每小時、每週日）＋人話解析＋「接下來 5 次執行時間」預覽（前端用 cron 解析函式庫計算）；enabled 開關。

**執行記錄**（頁面下半或 Tab；`GET /tasks/runs?task=`）：時間、任務、狀態 Tag（success／failed／running）、耗時、展開列顯示 `log`（等寬、深色底）。

**內建任務清單與翻譯**：`blob-gc`（回收未引用 blob，軟刪除）、`compact-blobs`（實際刪除檔案釋放空間）、`cleanup-policies`（套用清理規則）、`prune-expired`（清過期快取／session／token）、`rebuild-indexes`（重建各格式索引）、`backup`（有設定才出現）。

### 6.15 Cleanup Policies `/ui/admin/cleanup`

**列表**：name、format（`*` 顯示「全部」）、criteria 摘要（例：「30 天未下載 · 保留最新 5 版 · 排除 prerelease」）、套用的 repos Tags；「建立規則」。

**Drawer**（`POST`／`PUT /cleanup-policies`）：name、format（Select，含「全部」）、criteria：lastDownloadedDays、lastUpdatedDays（兩者 OR 關係要說明）、keepLatest（每個 name 保留最新 N 版）、nameRegex、versionRegex、prerelease（Radio：不限／只清 prerelease／排除 prerelease）。
**預覽區**（Drawer 底部或獨立 Modal）：選 repo → `POST /cleanup-policies/{id}/preview?repository=<name>&limit=200` → `{wouldDelete, packages[]}` 顯示「將刪除 N 個套件」與清單（namespace/name/version、最後下載），分頁。**未儲存的規則不能預覽**（先儲存）。
**指派**：Drawer 內 repo 多選（只列 format 相符）；差異化送 `PUT`／`DELETE /cleanup-policies/{id}/repositories/{name}`。

**業務邏輯**
- 至少一個 criteria 才能儲存。
- 規則只在 `cleanup-policies` 任務執行時生效：Drawer 頂端提示下次執行時間（從 `/tasks` 取），並提供「立即執行清理」按鈕（跳 Tasks 執行，先確認）。
- 這是最容易誤刪的地方：預覽結果 > 100 時用醒目黃色。

### 6.16 Routing Rules `/ui/admin/routing-rules`

**列表**：name、mode Tag（allow 綠／block 紅）、matchers 數（hover 顯示）、description、使用中的 repos（前端由 repositories 的 `routingRuleId` 反查）。

**Drawer**：name、description、mode Radio（`block`：符合任一 matcher 的路徑拒絕；`allow`：只有符合的才放行）、matchers 動態列表（regex，每列即時語法檢查）。
**測試面板**：輸入 path → `POST /routing-rules/test {mode, matchers, path}`（送目前表單內容，不需先儲存）→ `{allowed}` 顯示「允許／封鎖」＋命中的 matcher 高亮。

**業務邏輯**：被 repo 使用中的規則刪除 → 警告列出 repos，刪除後那些 repo 的 `routingRuleId` 變 null（後端處理）。

### 6.17 Backup / Restore `/ui/admin/backup`

**權限**：`app:system admin`（比 write 更高）。

**版面**
- **排程備份**卡（唯讀，來自 `/system/config` 的 `backup` 區）：目錄、是否含 blobs、保留數、cron；「在設定檔修改」說明；旁邊顯示 `backup` 任務最近執行。
- **立即下載備份**：勾選「包含 blobs」（警告：大小約 = 總 storage 用量）→ `GET /system/backup?blobs=true` 以瀏覽器下載（`<a download>`，帶 cookie）。
- **還原**危險區（紅框）：上傳 `.tar.gz`（Dragger）→ 第一段確認 Modal 說明「將覆蓋所有資料庫內容與 blobs，服務期間請停止 client 存取」→ 第二段輸入 `RESTORE` 才能按 → `POST /system/restore` multipart，上傳進度＋處理中 spinner（可能數分鐘）→ 完成後強制重新登入。

### 6.18 Webhooks `/ui/admin/webhooks`

**列表**：name、url、events Tags、repository 篩選（空＝全部）、enabled Switch、createdAt；「建立」；列動作：編輯、「送測試事件」（`POST /webhooks/{id}/test` → `{queued, message}`；事件是非同步送出，toast 顯示「已排入」，實際結果看 Logs）、刪除。

**Drawer**：name、url（https 建議）、events 多選（`asset.created`、`asset.deleted`、`package.deleted`、`repository.create`、`repository.update`、`repository.delete`、`repository.invalidate_cache`、`user.create`、`user.update`、`user.delete`、`task.failed`、`*`）、repository（Select，可空）、secret（密碼欄，`***` 規則）、enabled。
**說明區**：payload 範例 JSON、簽章 header `X-Holiaokho-Signature: sha256=<HMAC>`、驗簽範例（Node／Go 各一）。

### 6.19 Email `/ui/admin/email`

單一表單（`GET`／`PUT /email`）：enabled、host、port、username、password（`***`）、from、startTls／ssl（互斥 Radio：無／STARTTLS／SSL）、recipients（tags；任務失敗與配額警告會寄到這些人）。「寄測試信」（`POST /email/test {to}` → `{sent}` toast）。

### 6.20 System `/ui/admin/system/*`

- **Health** `/health`：同 Dashboard 卡片的表格版：check 名、狀態、message、（storage）用量；「重新檢查」按鈕；30s 輪詢。
- **Information** `/info`（`GET /system/info`）：分組 Descriptions：版本（version、commit、buildDate）、執行環境（`go`、`os`/`arch`、`cpus`、`hostname`、`pid`、`uptime`、`memory`：heapAllocBytes／sysBytes／goroutines／numGC）、資料庫（版本、連線數、各表列數）、storages、formats（26 個 Tag）、相依套件（可折疊表格）。右上角「複製為文字」。
- **Logs** `/logs`（`GET /system/logs?tail=500`）：深色等寬區、level 上色（ERROR 紅、WARN 黃、INFO 灰、DEBUG 淡）、頂部：tail 行數 Select（200／500／2000）、level 篩選（前端）、關鍵字過濾（前端）、「自動更新」Switch（2s，開啟時自動捲到底）、「複製全部」；旁邊 **Log level** Select（`GET`／`PUT /system/log-level`：debug／info／warn／error，即時生效，提示「重啟後回到設定檔值」）。
- **Configuration** `/config`（`GET /system/config`）：YAML 唯讀（語法高亮）；秘密已被後端遮蔽；頂部說明「修改請編輯設定檔或環境變數 `HOLIAOKHO_*`」。
- **Support ZIP** `/support`：一顆下載按鈕（`GET /system/support-zip`）＋內容物清單（system info、config（遮蔽）、最近 logs、health、repo 清單）＋「不含使用者資料與 blobs」。
- **Audit Log** `/audit`（`GET /audit?limit=`）：時間、actor（username；`anonymous`／token 前綴／`system`）、action（翻譯，例 `repository.create` → 「建立 repository」）、target 型別＋名稱（可點跳轉）、detail（JSON，展開列）；篩選 action／actor（前端）、limit Select；「載入更多」。

### 6.21 Migrate from Nexus `/ui/admin/migrate`

靜態說明頁（無 API），三個 Steps 卡片：
1. **匯入**（Nexus 還在跑時）：可複製的指令  
   `holiaokho import-nexus --nexus-db postgres://nexus:***@host:5432/nexus --nexus-blobs /nexus-data/blobs/default --link`  
   附參數說明（`--link` 用 hardlink 不複製檔案、`--dry-run`）、匯入內容清單（repos、users（密碼雜湊可沿用，首次登入自動升級）、roles、內容索引、blobs）。
2. **切換**：停 Nexus → Holiaokho 改監聽同一 port（K3s：改 Deployment image、沿用 Service／PVC 的 manifest 範例可複製）→ Docker port connector 對照表。
3. **驗證**：checklist（`mvn dependency:resolve`、`npm install`、`docker pull` 各一行）、「在 Repositories 列表確認匯入的 repo」連結。
底部「CI 不用改設定」對照表：URL 相同／Docker port 相同／帳密相同／REST `/service/rest/v1` 相容子集。

### 6.22 個人選單（頂欄右上）
- 顯示 username＋`via` Tag（session／token／ldap／oidc）。
- 項目：我的 Tokens、改密碼（local 使用者才顯示；`via` 為 ldap／oidc 隱藏）、語言、主題、登出。
- 匿名時此處是「登入」按鈕。

### 6.23 通知中心（頂欄鈴鐺）
純前端彙整，無專用 API：每 60s 拉 `/status/check` 與 `/tasks`，把「不健康的 check」與「lastStatus failed 的任務」列成通知；點擊跳對應頁；已讀狀態存 localStorage。沒有權限（非 Admin）則不顯示鈴鐺。

---

## 7. 關鍵流程（要畫 flow / 逐頁 mockup）

1. **首次安裝**：登入（admin/預設密碼）→ 強制改密碼 → Dashboard 空狀態引導 → 建立第一個 proxy（例如 Maven Central）→ 「使用方式」複製 settings.xml。
2. **Developer 找套件**：匿名進 Browse → 搜尋 `gson` → 看到在哪個 repo、哪些版本 → 複製下載 URL 或 client 設定。
3. **CI token**：Developer 登入 → 我的 Tokens → 建立 → 複製 secret → 說明放進 CI secret。
4. **Admin 設 LDAP**：Auth Settings → 填 LDAP → 測試登入成功 → 設 roleMapping → 把 ldap 加進 realm 順序。
5. **空間不夠**：Dashboard 配額警告 → Cleanup Policies 建規則 → 預覽 → 指派 → 執行 → Tasks 看結果 → compact-blobs 回收。
6. **從 Nexus 搬家**：Migrate 頁照做 → Repositories 列表看到匯入的 repo → 驗證。

---

## 8. 多語系與在地化

- 語言：**en、zh-TW、zh-CN、ja、ko**，右上角切換，記住偏好；預設跟瀏覽器。
- 後端錯誤回 `{code, message, params}`，前端依 `code` 查翻譯（例：`repo.redeploy_denied` → 「此 repository 不允許重複部署同版本」）。設計時錯誤訊息預留**兩行**空間。
- 文字長度：日文／中文通常較短，英文／韓文較長；按鈕與表格欄寬要能容納 +30%。
- 日期時間：依語系格式，顯示相對時間（「3 分鐘前」）並 hover 顯示絕對時間。
- 檔案大小：KB/MB/GB 依語系。
- 不需要 RTL。

---

## 9. 版面與裝置

- **桌面優先**（Admin 都在桌機），寬度 1280–1920；資料表多、要善用寬度。
- 平板（768–1024）：導覽收合、表格可橫向捲動、表單單欄。
- 手機（<480）：只保證 Browse／Search／登入／我的 Tokens 可用；Admin 頁面可降級但不必精雕。
- **Light 與 Dark 兩套主題**皆為一等公民（開發者常用 dark）。
- 無障礙：對比 ≥ 4.5:1、鍵盤可操作所有表單與表格、焦點可見、icon 都有文字標籤。

---

## 10. 狀態與細節（每個列表／表單都要有）

- Loading（skeleton）、空狀態（附下一步動作）、錯誤（可重試、顯示錯誤碼）、權限不足（不顯示或友善說明）。
- 破壞性操作（刪 repo、刪 user、還原備份、執行 cleanup）：Modal 要求輸入名稱或勾選確認。
- 大量資料：repositories 可能 100+、packages 可能 100k+、assets 更多——表格要分頁／虛擬捲動、搜尋要防抖。
- 複製到剪貼簿的元件會非常多（URL、指令、checksum、token）：需要統一的「可複製文字」元件與成功提示。
- 即時性：Tasks 執行中、Logs tail、health 卡片要能輪詢更新（不需 websocket）。
- Docker 特殊：repo 有多種存取位址（port 模式 `host:5000/image`、path 模式 `host/repo/image`、subdomain），「使用方式」要清楚分開。

---

## 11. 技術限制（設計需知道）

- SPA 掛在 `/ui/`（其他路徑是套件 client 用的 `/repository/...`、`/v2/...`），build 後打包進 Go binary；沒有 SSR。
- 所有資料都來自 `/api/v1`（OpenAPI：`/api/v1/openapi.yaml`），無 GraphQL、無 websocket。
- 認證：session cookie（登入表單）或 Bearer token；OIDC 登入是整頁跳轉回 `/api/v1/auth/oidc/callback` 再導回 `next`。
- 匿名使用者可能有讀權限：UI 要能在未登入狀態渲染 Browse／Search。
- 圖示：各格式 icon 需自行設計（不可用第三方商標）；可用簡潔的字母／幾何圖形＋生態系慣用色（Java 橙、npm 紅、Docker 藍、Python 黃藍、Go 青、Rust 橙、Ruby 紅…）。

---

## 12. 期望交付

1. **設計系統**：色彩（含 light/dark）、字級、間距、圓角、陰影、狀態色；對應到 Ant Design token 名稱。
2. **Logo / wordmark** 提案 2–3 款與 favicon。
3. **頁面 layout**：第 6 節每一頁的桌面版（重點頁另附平板版）；第 7 節六個流程的逐頁 mockup。
4. **元件規範**：format icon 集（26 個）、type 標籤（hosted/proxy/group）、可複製文字、健康狀態卡片、privilege 編輯器、cron 編輯器、一次性 secret Modal、預覽刪除清單。
5. **空狀態／錯誤／loading** 的通用樣式。
6. 一份「設計說明」讓前端工程師照做（含 i18n 與 dark mode 注意事項）。

---

## 附錄 A：對照 Nexus 的 UI（作為「不要長這樣」的參考）

Nexus 3 的 UI 是左側設定樹（Repository／Security／Support／System）＋右側 ExtJS 表格，密度高但老舊、對開發者不友善（沒有「怎麼用」的引導、搜尋分散）。我們保留它「管理項目齊全」的優點，改善：Browse 與 Search 給非管理者的體驗、每個 repo 的使用方式片段、預覽式的清理、現代的 dark mode 與多語系。

## 附錄 B：名詞翻譯建議（zh-TW）

Repository → 儲存庫（或直接用 Repository）、Hosted → 自有、Proxy → 代理快取、Group → 群組、Package → 套件、Asset → 檔案、Storage → 儲存空間、Cleanup Policy → 清理規則、Routing Rule → 路由規則、Content Selector → 內容選擇器、Token → 存取權杖、Realm → 認證來源、Audit Log → 稽核記錄、Blob → 內容區塊。其他語言由翻譯階段處理，但設計稿可用 en 與 zh-TW 兩種文案示範。
