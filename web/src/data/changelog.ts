// What changed in each release, in every locale the UI speaks.
//
// This lives outside the i18n bundles on purpose: the list grows with every
// release and would bloat the translation files, and an entry that is missing
// a locale should fall back to English rather than fail the locale check.
export type Locale = 'en' | 'zh-TW' | 'zh-CN' | 'ja' | 'ko'
export type Text = Partial<Record<Locale, string>> & { en: string }

export interface Release {
  version: string
  date: string
  headline?: Text
  changes: { kind: 'added' | 'fixed' | 'changed'; text: Text }[]
}

export const releases: Release[] = [
  {
    version: '1.1.1',
    date: '2026-09-14',
    changes: [
      {
        kind: 'added',
        text: {
          en: 'Package details now list what is attached to a Docker image — signatures, SBOMs and attestations — with the kind of each one named rather than left as a media type to decipher.',
          'zh-TW': '套件詳情會列出 Docker image 上掛了什麼 —— 簽章、SBOM、建置證明 —— 並直接標出種類,不用自己去解讀 media type。',
          'zh-CN': '套件详情会列出 Docker 镜像上挂了什么 —— 签名、SBOM、构建证明 —— 并直接标出种类,不用自己去解读 media type。',
          ja: 'パッケージ詳細に、イメージに紐付いた署名・SBOM・証明の一覧が表示されます。メディアタイプを読み解かなくても種類が分かります。',
          ko: '패키지 상세에 이미지에 붙은 서명·SBOM·증명 목록이 표시됩니다. 미디어 타입을 해석하지 않아도 종류를 알 수 있습니다.',
        },
      },
    ],
  },
  {
    version: '1.1.0',
    date: '2026-09-14',
    headline: {
      en: 'Docker and OCI repositories can now hold signatures, SBOMs and attestations alongside the images they describe.',
      'zh-TW': 'Docker 與 OCI repository 現在可以把簽章、SBOM 與建置證明,和它們描述的 image 放在一起。',
      'zh-CN': 'Docker 与 OCI repository 现在可以把签名、SBOM 与构建证明,和它们描述的镜像放在一起。',
      ja: 'Docker / OCI リポジトリで、署名・SBOM・ビルド証明をそれが説明するイメージと一緒に保持できるようになりました。',
      ko: 'Docker/OCI 리포지터리에서 서명·SBOM·빌드 증명을 대상 이미지와 함께 보관할 수 있습니다.',
    },
    changes: [
      {
        kind: 'added',
        text: {
          en: 'The OCI referrers API. Tools like cosign and syft attach their output to an image as a separate manifest; this is the endpoint that finds them again. Works on proxy repositories too, where the upstream is asked and the answer cached.',
          'zh-TW': 'OCI referrers API。cosign、syft 這類工具會把產出的簽章或 SBOM 以獨立 manifest 掛在 image 上,這個端點負責把它們找回來。proxy repository 也支援,會詢問上游並快取結果。',
          'zh-CN': 'OCI referrers API。cosign、syft 这类工具会把产出的签名或 SBOM 以独立 manifest 挂在镜像上,这个端点负责把它们找回来。proxy repository 也支持,会询问上游并缓存结果。',
          ja: 'OCI referrers API。cosign や syft は署名や SBOM を別のマニフェストとしてイメージに紐付けます。このエンドポイントがそれらを見つけ出します。プロキシでも動作し、上流に問い合わせた結果をキャッシュします。',
          ko: 'OCI referrers API. cosign이나 syft는 서명과 SBOM을 별도 매니페스트로 이미지에 붙입니다. 이 엔드포인트가 그것들을 다시 찾아줍니다. 프록시에서도 동작하며 업스트림 응답을 캐시합니다.',
        },
      },
    ],
  },
  {
    version: '1.0.5',
    date: '2026-09-14',
    changes: [
      {
        kind: 'fixed',
        text: {
          en: 'A vulnerability in an indirect dependency used by package signing (cloudflare/circl, GO-2026-4550). Found by the vulnerability scanner on its first run in CI.',
          'zh-TW': '套件簽章所用的間接相依套件有漏洞(cloudflare/circl,GO-2026-4550)。由 CI 上第一次執行的漏洞掃描找到。',
          'zh-CN': '软件包签名所用的间接依赖存在漏洞(cloudflare/circl,GO-2026-4550)。由 CI 上第一次执行的漏洞扫描发现。',
          ja: 'パッケージ署名が使う間接依存の脆弱性 (cloudflare/circl, GO-2026-4550)。CI で初めて実行した脆弱性スキャンが検出しました。',
          ko: '패키지 서명이 사용하는 간접 의존성의 취약점 (cloudflare/circl, GO-2026-4550). CI에서 처음 실행한 취약점 검사가 찾아냈습니다.',
        },
      },
    ],
  },
  {
    version: '1.0.4',
    date: '2026-09-14',
    changes: [
      {
        kind: 'changed',
        text: {
          en: 'The Kubernetes manifests now run both containers as a non-root user with a read-only root filesystem and no Linux capabilities. Verified by running them that way, not just by passing a scanner.',
          'zh-TW': 'Kubernetes manifest 現在讓兩個容器都以非 root 身分、唯讀根檔案系統、且不保留任何 Linux capability 執行。這是實際跑起來驗證過的,不只是讓掃描器過關。',
          'zh-CN': 'Kubernetes manifest 现在让两个容器都以非 root 身份、只读根文件系统、且不保留任何 Linux capability 运行。这是实际运行验证过的,不只是让扫描器通过。',
          ja: 'Kubernetes マニフェストで、両方のコンテナが非 root ユーザー・読み取り専用ルートファイルシステム・Linux ケーパビリティなしで動作するようになりました。スキャナを通すためではなく、実際にその構成で動かして確認しています。',
          ko: 'Kubernetes 매니페스트에서 두 컨테이너 모두 비 root 사용자, 읽기 전용 루트 파일시스템, Linux capability 없이 실행됩니다. 스캐너를 통과시키려는 것이 아니라 실제로 그 구성으로 동작을 확인했습니다.',
        },
      },
      {
        kind: 'added',
        text: {
          en: 'Every push is scanned: Gitleaks over the full history, Trivy for dependencies, configuration and the image, plus go vet, govulncheck and staticcheck.',
          'zh-TW': '每次推送都會掃描:Gitleaks 掃完整 git 歷史,Trivy 掃相依套件、設定與映像檔,另有 go vet、govulncheck 與 staticcheck。',
          'zh-CN': '每次推送都会扫描:Gitleaks 扫完整 git 历史,Trivy 扫依赖、配置与镜像,另有 go vet、govulncheck 与 staticcheck。',
          ja: 'プッシュのたびにスキャンします: 全履歴に対する Gitleaks、依存関係・設定・イメージに対する Trivy、さらに go vet・govulncheck・staticcheck。',
          ko: '푸시할 때마다 검사합니다: 전체 히스토리에 대한 Gitleaks, 의존성·설정·이미지에 대한 Trivy, 그리고 go vet·govulncheck·staticcheck.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'The per-format test scripts could not run against a fresh server, because the forced password change introduced in 1.0.0 locked them out.',
          'zh-TW': '各格式的測試腳本無法對全新的伺服器執行——1.0.0 加入的強制改密碼把它們擋在門外。',
          'zh-CN': '各格式的测试脚本无法对全新的服务器执行——1.0.0 加入的强制改密码把它们挡在门外。',
          ja: 'フォーマット別のテストスクリプトが新規サーバーに対して実行できませんでした。1.0.0 で入れた強制パスワード変更に阻まれていたためです。',
          ko: '포맷별 테스트 스크립트가 새 서버에서 실행되지 않았습니다. 1.0.0에서 도입한 강제 비밀번호 변경에 막혀 있었습니다.',
        },
      },
    ],
  },
  {
    version: '1.0.3',
    date: '2026-09-13',
    changes: [
      {
        kind: 'added',
        text: {
          en: 'New installs get a ghcr.io proxy and a docker-group on port 8082, so a Docker daemon can reach both Docker Hub and ghcr through one address.',
          'zh-TW': '全新安裝會自動建立 ghcr.io 代理與 docker-group(綁定 8082),讓 Docker 透過同一個位址就能取得 Docker Hub 與 ghcr 的 image。',
          'zh-CN': '全新安装会自动建立 ghcr.io 代理与 docker-group(绑定 8082),让 Docker 通过同一个地址就能取得 Docker Hub 与 ghcr 的镜像。',
          ja: '新規インストールで ghcr.io プロキシと docker-group(ポート 8082)が作られ、Docker Hub と ghcr の両方に一つのアドレスで到達できます。',
          ko: '새로 설치하면 ghcr.io 프록시와 docker-group(8082 포트)이 만들어져, 하나의 주소로 Docker Hub와 ghcr 모두에 접근할 수 있습니다.',
        },
      },
    ],
  },
  {
    version: '1.0.2',
    date: '2026-09-13',
    changes: [
      {
        kind: 'added',
        text: {
          en: 'Release notes: the version number now opens this page.',
          'zh-TW': '更新紀錄:點側邊欄的版本號就會開啟這一頁。',
          'zh-CN': '更新记录:点侧边栏的版本号就会打开这一页。',
          ja: 'リリースノート: サイドバーのバージョン番号からこのページを開けます。',
          ko: '릴리스 노트: 사이드바의 버전 번호를 누르면 이 페이지가 열립니다.',
        },
      },
      {
        kind: 'added',
        text: {
          en: 'About Holiaokho is now in the user menu as well.',
          'zh-TW': '使用者選單裡也能進入「關於好料庫」。',
          'zh-CN': '用户菜单里也能进入「关于好料库」。',
          ja: 'ユーザーメニューからも「Holiaokho について」を開けるようになりました。',
          ko: '사용자 메뉴에서도 Holiaokho 소개로 갈 수 있습니다.',
        },
      },
    ],
  },
  {
    version: '1.0.1',
    date: '2026-09-13',
    changes: [
      {
        kind: 'added',
        text: {
          en: 'A way into the About page from the login screen, for people who have not signed in yet.',
          'zh-TW': '登入頁加入「關於好料庫」入口,還沒登入的人也看得到。',
          'zh-CN': '登录页加入「关于好料库」入口,还没登录的人也看得到。',
          ja: 'ログイン画面から「Holiaokho について」へ入れるようにしました。',
          ko: '로그인 화면에서도 Holiaokho 소개로 들어갈 수 있습니다.',
        },
      },
    ],
  },
  {
    version: '1.0.0',
    date: '2026-09-13',
    headline: {
      en: 'First stable release. Holiaokho has been carrying a company CI/CD pipeline in place of Nexus.',
      'zh-TW': '第一個正式版本。好料庫已在正式環境取代 Nexus,承載公司的 CI/CD。',
      'zh-CN': '第一个正式版本。好料库已在正式环境取代 Nexus,承载公司的 CI/CD。',
      ja: '最初の安定版。Holiaokho は本番環境で Nexus に代わって CI/CD を支えています。',
      ko: '첫 정식 릴리스. Holiaokho는 운영 환경에서 Nexus를 대신해 CI/CD를 받치고 있습니다.',
    },
    changes: [
      {
        kind: 'added',
        text: {
          en: 'A password somebody else chose — the first admin password, or an administrator reset — must be replaced before the account can do anything else.',
          'zh-TW': '別人幫你設的密碼(初始管理者密碼、管理員重設的密碼)必須先換掉,帳號才能做其他事。',
          'zh-CN': '别人替你设的密码(初始管理员密码、管理员重设的密码)必须先换掉,账号才能做其他事。',
          ja: '他人が設定したパスワード(初期管理者パスワードや管理者によるリセット)は、変更するまでアカウントを使えません。',
          ko: '다른 사람이 정한 비밀번호(초기 관리자 비밀번호, 관리자 재설정)는 바꾸기 전까지 계정을 쓸 수 없습니다.',
        },
      },
      {
        kind: 'added',
        text: {
          en: 'An About page explaining what Holiaokho is and where the name comes from.',
          'zh-TW': '新增「關於」頁,說明好料庫是什麼,以及名字的由來。',
          'zh-CN': '新增「关于」页,说明好料库是什么,以及名字的由来。',
          ja: 'Holiaokho が何であり、名前がどこから来たのかを説明する「About」ページを追加しました。',
          ko: 'Holiaokho가 무엇이고 이름이 어디서 왔는지 설명하는 소개 페이지를 추가했습니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'In dark mode an "on" switch looked switched off, which made every online repository appear offline.',
          'zh-TW': '深色模式下開啟的開關看起來像關閉的,讓所有上線的 repository 都像是離線。',
          'zh-CN': '深色模式下打开的开关看起来像关闭的,让所有上线的 repository 都像是离线。',
          ja: 'ダークモードでオンのスイッチがオフに見え、稼働中のリポジトリがすべて停止中のように見えていました。',
          ko: '다크 모드에서 켜진 스위치가 꺼진 것처럼 보여, 온라인 리포지터리가 모두 오프라인처럼 보였습니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'On wide screens the dashboard cards left a band of empty space instead of filling the row.',
          'zh-TW': '寬螢幕下總覽頁的卡片沒有填滿整列,右側會留下一片空白。',
          'zh-CN': '宽屏下概览页的卡片没有填满整行,右侧会留下一片空白。',
          ja: '幅の広い画面でダッシュボードのカードが行を埋めず、右側に空白が残っていました。',
          ko: '넓은 화면에서 대시보드 카드가 행을 채우지 않고 오른쪽에 빈 공간이 남았습니다.',
        },
      },
      {
        kind: 'changed',
        text: {
          en: 'The running build now reports its own version in the interface, the API and the logs.',
          'zh-TW': '執行中的版本現在會在介面、API 與日誌裡回報自己真正的版號。',
          'zh-CN': '运行中的版本现在会在界面、API 与日志里报告自己真正的版本号。',
          ja: '実行中のビルドが、画面・API・ログのいずれでも自身のバージョンを正しく報告します。',
          ko: '실행 중인 빌드가 화면과 API, 로그에서 자신의 버전을 올바르게 알립니다.',
        },
      },
    ],
  },
]

export function pick(text: Text, locale: string): string {
  return text[locale as Locale] ?? text.en
}
