[English](security.md) | 繁體中文

# Holiaokho 安全模型與審查記錄

> 2026-09-13 全後端審查。這份文件說明信任邊界、已實作的防護、審查時找到並修掉的問題，以及部署時要注意的事。

## 信任邊界

| 來源 | 信任程度 |
|---|---|
| 設定檔／環境變數／K8s Secret | 完全信任（管理者） |
| `admin` 與有 `app:*` 權限的使用者 | 信任其管理操作（設 proxy 上游、webhook URL、cleanup regex 等都可能影響伺服器行為，這是設計） |
| 有 repo `write` 權限的使用者 | 可上傳內容；**內容視為不可信**（見 XSS 防護） |
| 匿名／`read` 使用者 | 只能讀取被授權的 repo；**任何輸入都視為不可信** |
| 上游 registry（Maven Central、Docker Hub…） | 半信任：回應內容會被快取並轉發，但不執行；上游 metadata 裡的 URL 只用於再抓取（等同 Nexus） |
| 反向代理 | 只有在 `server.trusted_proxies` 內的 peer 才相信它送來的 `X-Forwarded-For` |

## 已實作的防護

**認證與授權**
- 密碼 argon2id；匯入的 Nexus Shiro 雜湊驗證後自動升級；比較用常數時間
- 密碼複雜度政策（`auth.settings.password`，API 可調）：預設最少 12 碼、需含大小寫與數字、不可包含使用者名稱、拒絕常見弱密碼；建立使用者、admin 重設、自助改密碼一律套用，違反時回 `password.*` code 與 params。Bootstrap 的 admin 密碼不擋但會在 log 警告
- User token 只存 SHA-256；Docker/npm/NuGet/cargo/gem/conan 等格式的 token 都對應到同一套 token
- 登入失敗限流（依 client IP，預設 10 次／分鐘，回 429）
- RBAC：`target` × `actions`，content selector 可限制到路徑；匿名是明確的角色，可整個關閉
- Session cookie：HttpOnly、SameSite=Lax、HTTPS 時 Secure；登入產生新 session id
- OIDC：state cookie 防 CSRF、`next` 只允許站內路徑；LDAP filter 有 escape；Rut Auth 必須設定 `trustedProxies` 否則忽略 header
- API 一律經過權限檢查；沒權限的 repo 在搜尋結果中被過濾

**輸入處理**
- 所有 SQL 參數化；LIKE 樣式有 escape
- 儲存路徑經 `CleanPath`（拒絕 `..`、空段、控制字元）；blob 以 sha256 為 key，使用者永遠碰不到實體路徑
- Docker upload id、npm 套件名、Docker image 名、LFS oid 都有格式驗證
- 上傳與上游讀取都有大小上限；zip／tar 解析只讀需要的檔案
- 上游位址只來自管理者設定或伺服器自己解析的 metadata，**不接受 client 指定的 URL**（Terraform module、Go sumdb 已修正）

**瀏覽器防護**
- 全站 `X-Content-Type-Options: nosniff`
- `/repository/*`、`/v2/*` 回應加 `Content-Security-Policy: sandbox; default-src 'none'`：上傳的 HTML 在瀏覽器中不會以 Holiaokho 的 origin 執行 script（儲存型 XSS 防護）
- API／UI：`X-Frame-Options: DENY`、`Referrer-Policy`、API 的 CSP `default-src 'none'`
- 目錄列表與 simple index 的輸出都有 HTML escape

**機密**
- 上游密碼、簽章私鑰、LDAP/OIDC/SMTP 密碼、S3 secretKey 以 AES-256-GCM 加密後入庫（`secrets.key`），支援輪替
- API 回應一律遮蔽為 `***`；`/system/config` 遮蔽 DB URL、S3、admin 密碼、http_proxy 內的帳密
- 備份檔含密文，需要同一把 key 才能使用
- TLS 最低 1.2

## 2026-09-13 審查發現與修正

| 嚴重度 | 問題 | 修正 |
|---|---|---|
| 高 | Docker chunked upload 的 `uploads/<id>` 未驗證 → 可用 `../` 讀寫 storage 目錄外檔案 | id 必須是 UUID；storage 層另加 `ValidUploadID` 雙重把關 |
| 高 | Terraform module proxy 接受 client 的 `?upstream=` → SSRF 與快取投毒 | 改由伺服器向上游 registry 重新解析下載位址 |
| 中 | Go proxy `/sumdb/<host>/…` 轉發到任意 host | 只允許 `sum.golang.org` / `sum.golang.google.cn` |
| 中 | 上傳 HTML 到 hosted repo → 儲存型 XSS | 內容回應加 CSP sandbox + nosniff |
| 中 | OIDC `next=//evil.com` open redirect | 拒絕 `//`、`/\` 開頭 |
| 中 | `X-Forwarded-For` 無條件信任 → 偽造繞過登入限流 | 只信任 `trusted_proxies` 內 peer，取最右側不可信 hop |
| 中 | Rut Auth 允許空 `trustedProxies`（＝信任所有人） | 空清單時忽略 header 並記錄警告 |
| 低 | 缺安全 header、cookie 無 Secure、TLS 無最低版本、`http_proxy` 帳密未遮蔽、npm 名稱未驗證、restore 依賴 superuser | 全部補上 |

## 部署注意事項

1. **`HOLIAOKHO_SECRET_KEY`** 用 K8s Secret 管理並備份；遺失＝所有上游密碼與簽章金鑰不可解。
2. **`server.trusted_proxies`** 縮到實際的 ingress／proxy 位址；預設是私有網段（方便叢集內部署），代表同網段的節點可影響 rate limit 判定的 IP。
3. 對外服務**一定放在 TLS 之後**（反向代理或 `server.tls_*`）；Basic auth 與 token 走明文 HTTP 會被竊聽。
4. 第一次啟動後**立刻改 admin 密碼**（health check 會一直警告）。
5. 匿名讀取預設開啟（為了無痛替代 Nexus）；不需要就設 `auth.anonymous_enabled: false`。
6. `/metrics` 與 `/service/metrics/prometheus` 不需認證（只有計數）；不想外露就在 ingress 擋。
7. 管理者可設定的 proxy 上游、webhook URL 會由伺服器主動連線——這是功能，但代表 admin 帳號等於能讓伺服器對內網發請求；admin 帳號要嚴格控管。

## 尚未做（已知）

- 沒有全站 API 速率限制（只有登入）
- Docker foreign layer 直接由 client 抓上游（與 Nexus 預設相同）
- 上游 metadata 內的 URL（Helm index、Composer dist、Ansible download_url…）會被伺服器抓取：惡意上游可讓伺服器連到它指定的位址；proxy 上游只能由 admin 設定，風險等同 Nexus
