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
