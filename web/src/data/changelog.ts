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
    version: '1.4.0',
    date: '2026-09-27',
    changes: [
      {
        kind: 'added',
        text: {
          en: 'Deploy through a group, in every format: mvn deploy, npm publish, twine upload, docker push and the rest, sent to a group, land in its first hosted member — one URL in a build for both directions. It takes write permission on the group and on that member; npm login and audit, and Git LFS batch requests, are still answered by the group.',
          'zh-TW': '所有格式都能部署到 group：對 group 執行 mvn deploy、npm publish、twine upload、docker push 等，內容會存進它的第一個 hosted 成員，建置只要設定一個網址就能下載也能上傳。需要同時對 group 與該成員有寫入權限；npm login、audit 與 Git LFS batch 仍由 group 本身回應。',
          'zh-CN': '所有格式都能部署到 group：对 group 执行 mvn deploy、npm publish、twine upload、docker push 等，内容会存入它的第一个 hosted 成员，构建只需配置一个地址就能下载也能上传。需要同时对 group 与该成员有写入权限；npm login、audit 与 Git LFS batch 仍由 group 本身响应。',
          ja: 'すべての形式でグループへデプロイできます。グループに対する mvn deploy、npm publish、twine upload、docker push などは最初のホストメンバーに保存されるため、ビルドは 1 つの URL で取得も公開もできます。グループとそのメンバーの両方への書き込み権限が必要です。npm login と audit、Git LFS の batch リクエストは引き続きグループが応答します。',
          ko: '모든 형식에서 그룹으로 배포할 수 있습니다. 그룹에 대한 mvn deploy, npm publish, twine upload, docker push 등은 첫 번째 호스티드 멤버에 저장되므로, 빌드는 URL 하나로 받기와 올리기를 모두 할 수 있습니다. 그룹과 그 멤버 모두에 대한 쓰기 권한이 필요합니다. npm login과 audit, Git LFS batch 요청은 계속 그룹이 응답합니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'A group\'s members are an ordered list you can drag, or move with arrows. A member added to a group now goes last; before, it went first, so a proxy added for a handful of packages was quietly asked before Maven Central for everything.',
          'zh-TW': 'Group 的成員改為有順序的清單，可拖曳或用箭頭調整。新加入的成員現在會排在最後；過去會排在最前面，於是為了少數套件加的 proxy，會在不知不覺中比 Maven Central 更早被查詢所有套件。',
          'zh-CN': 'Group 的成员改为有顺序的列表，可拖动或用箭头调整。新加入的成员现在会排在最后；过去会排在最前面，于是为了少数包加的 proxy，会在不知不觉中比 Maven Central 更早被查询所有包。',
          ja: 'グループのメンバーを順序付きのリストにし、ドラッグまたは矢印で並べ替えられるようにしました。追加したメンバーは末尾に入ります。以前は先頭に入っていたため、一部のパッケージのために追加したプロキシが、すべてのパッケージについて Maven Central より先に問い合わされていました。',
          ko: '그룹 멤버를 순서가 있는 목록으로 바꿔 드래그나 화살표로 순서를 조정할 수 있습니다. 새로 추가한 멤버는 이제 맨 뒤에 들어갑니다. 전에는 맨 앞에 들어가서, 몇몇 패키지를 위해 추가한 프록시가 모든 패키지에 대해 Maven Central보다 먼저 조회되었습니다.',
        },
      },
    ],
  },
  {
    version: '1.3.5',
    date: '2026-09-26',
    changes: [
      {
        kind: 'added',
        text: {
          en: 'Every table can be sorted by clicking a column header. Search results, a repository\'s packages and the Vulnerabilities list are sorted by the server, so the order covers all results rather than just the page on screen; versions sort as versions (3.9 before 3.12).',
          'zh-TW': '所有表格都可以點擊標題列排序。搜尋結果、repository 的套件清單與「漏洞」清單由伺服器排序，排的是全部結果而不只是目前這一頁；版本號會依版本大小排序（3.9 排在 3.12 前面）。',
          'zh-CN': '所有表格都可以点击标题栏排序。搜索结果、repository 的包列表与「漏洞」列表由服务器排序，排的是全部结果而不只是当前这一页；版本号按版本大小排序（3.9 排在 3.12 前面）。',
          ja: 'すべての表で列見出しをクリックして並べ替えられます。検索結果、リポジトリのパッケージ一覧、「脆弱性」一覧はサーバー側で並べ替えるため、表示中のページだけでなく全件が対象です。バージョンはバージョン順に並びます（3.9 は 3.12 より前）。',
          ko: '모든 표에서 열 머리글을 클릭해 정렬할 수 있습니다. 검색 결과, 저장소의 패키지 목록, 「취약점」 목록은 서버에서 정렬하므로 화면의 한 페이지가 아니라 전체 결과가 대상입니다. 버전은 버전 순으로 정렬됩니다(3.9가 3.12보다 앞).',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'A cron schedule\'s "next runs" preview was worked out in your browser\'s time zone, but tasks run on the server\'s clock: on a server in UTC, "0 2 * * *" was previewed as 02:00 your time while it actually ran at 02:00 UTC. The preview now comes from the server, names the server\'s time zone, and shows each run in your time next to the server\'s.',
          'zh-TW': 'cron 排程的「接下來執行時間」預覽原本以瀏覽器的時區計算，但任務是依伺服器時鐘執行：伺服器在 UTC 時，「0 2 * * *」預覽顯示為你的 02:00，實際卻在 UTC 02:00 執行。現在預覽改由伺服器計算，會標明伺服器時區，並同時列出你的時間與伺服器時間。',
          'zh-CN': 'cron 计划的「接下来执行时间」预览原本按浏览器的时区计算，但任务是按服务器时钟执行：服务器在 UTC 时，「0 2 * * *」预览显示为你的 02:00，实际却在 UTC 02:00 执行。现在预览改由服务器计算，会标明服务器时区，并同时列出你的时间与服务器时间。',
          ja: 'cron スケジュールの「次回実行」プレビューはブラウザのタイムゾーンで計算されていましたが、タスクはサーバーの時計で実行されます。サーバーが UTC の場合、「0 2 * * *」はあなたの 02:00 と表示されながら実際は UTC 02:00 に実行されていました。プレビューはサーバーが計算し、サーバーのタイムゾーンを明示して、あなたの時刻とサーバーの時刻を並べて表示します。',
          ko: 'cron 일정의 「다음 실행」 미리보기가 브라우저 시간대로 계산되었지만 작업은 서버 시계로 실행됩니다. 서버가 UTC이면 「0 2 * * *」가 사용자 시간 02:00으로 표시되면서 실제로는 UTC 02:00에 실행되었습니다. 이제 미리보기를 서버가 계산하고 서버 시간대를 표시하며, 각 실행을 사용자 시간과 서버 시간으로 함께 보여 줍니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'The task filter above the run history is a dropdown; as a row of buttons it had grown wider than the page.',
          'zh-TW': '執行記錄上方的任務篩選改為下拉選單；原本的按鈕列在任務變多後已超出頁面寬度。',
          'zh-CN': '执行记录上方的任务筛选改为下拉菜单；原来的按钮栏在任务变多后已超出页面宽度。',
          ja: '実行履歴の上にあるタスクの絞り込みをドロップダウンにしました。ボタンの列はタスクが増えてページ幅を超えていました。',
          ko: '실행 기록 위의 작업 필터를 드롭다운으로 바꿨습니다. 버튼 줄은 작업이 늘어 페이지 폭을 넘었습니다.',
        },
      },
    ],
  },
  {
    version: '1.3.4',
    date: '2026-09-26',
    changes: [
      {
        kind: 'added',
        text: {
          en: 'The Vulnerabilities page and every report show when each package was last used — last downloaded, or stored if it never has been — so a vulnerable package that builds still pull can be told from one nobody has touched in months.',
          'zh-TW': '「漏洞」頁面與所有報告都會顯示每個套件的「最後使用」時間(最後一次被下載;從未下載過則為存入的時間),可以分辨出仍在被建置使用的有漏洞套件,和已經好幾個月沒人碰的套件。',
          'zh-CN': '「漏洞」页面与所有报告都会显示每个包的「最后使用」时间(最后一次被下载;从未下载过则为存入的时间),可以区分出仍在被构建使用的有漏洞包,和已经好几个月没人碰的包。',
          ja: '「脆弱性」ページとすべてのレポートに、各パッケージの最終利用日時(最後にダウンロードされた日時、未ダウンロードなら保存日時)を表示します。ビルドがまだ取得している脆弱なパッケージと、何か月も使われていないものを区別できます。',
          ko: '「취약점」 페이지와 모든 보고서에 각 패키지의 마지막 사용 시각(마지막 다운로드 시각, 다운로드된 적이 없으면 저장 시각)을 표시합니다. 빌드가 아직 받아 가는 취약한 패키지와 몇 달째 아무도 쓰지 않는 패키지를 구별할 수 있습니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'With the vulnerability scan scheduled once a day, each package was rechecked only every other day: a run stamps its packages as it finishes, so at the next day\'s run they were a few seconds short of 24 hours old and skipped. A package now counts as due after 20 hours, so a daily schedule rechecks everything daily.',
          'zh-TW': '漏洞掃描排程設為每天一次時,每個套件實際上是每兩天才重新檢查:掃描會在結束時記下檢查時間,隔天同一時間開始掃描時,那些套件差幾秒才滿 24 小時而被略過。現在超過 20 小時就算到期,每天一次的排程每天都會完整重掃。',
          'zh-CN': '漏洞扫描计划设为每天一次时,每个包实际上是每两天才重新检查:扫描会在结束时记下检查时间,隔天同一时间开始扫描时,那些包差几秒才满 24 小时而被跳过。现在超过 20 小时就算到期,每天一次的计划每天都会完整重扫。',
          ja: '脆弱性スキャンを 1 日 1 回に設定すると、各パッケージは実質 2 日ごとにしか再チェックされていませんでした。実行終了時に時刻を記録するため、翌日の同時刻には 24 時間に数秒足りず対象外になっていました。20 時間を過ぎれば対象とするようにし、1 日 1 回の設定で毎日すべて再チェックされます。',
          ko: '취약점 스캔을 하루 한 번으로 설정하면 각 패키지가 사실상 이틀에 한 번만 재확인되었습니다. 실행이 끝날 때 시각을 기록하므로 다음 날 같은 시각에는 24시간에 몇 초 모자라 건너뛰었습니다. 이제 20시간이 지나면 대상이 되어 하루 한 번 설정으로도 매일 모두 재확인합니다.',
        },
      },
    ],
  },
  {
    version: '1.3.3',
    date: '2026-09-26',
    changes: [
      {
        kind: 'fixed',
        text: {
          en: 'Vulnerability findings, their summary and report exports now require signing in. With anonymous access on — as it usually is, so builds can pull without credentials — anyone who could reach the server could read or download a list of the known vulnerabilities in the packages it holds. Anonymous pulls are unaffected.',
          'zh-TW': '漏洞清單、摘要與報告匯出現在都必須登入才能使用。以前在開放匿名存取時(通常都會開,讓建置不需帳密就能拉套件),任何連得到伺服器的人都能看到或下載已存放套件的已知漏洞清單。匿名拉取套件不受影響。',
          'zh-CN': '漏洞列表、摘要与报告导出现在都必须登录才能使用。以前在开放匿名访问时(通常都会开,让构建不需账密就能拉包),任何连得到服务器的人都能看到或下载已存储包的已知漏洞列表。匿名拉取包不受影响。',
          ja: '脆弱性の一覧・概要・レポート出力はサインインが必要になりました。匿名アクセスが有効な場合(ビルドが認証なしで取得できるよう通常は有効)、サーバーに到達できる誰もが既知の脆弱性の一覧を閲覧・ダウンロードできていました。匿名での取得は影響を受けません。',
          ko: '취약점 목록·요약·보고서 내보내기는 이제 로그인해야 사용할 수 있습니다. 익명 접근이 켜져 있으면(빌드가 자격 증명 없이 받을 수 있도록 보통 켜 둠) 서버에 접근할 수 있는 누구나 알려진 취약점 목록을 보거나 내려받을 수 있었습니다. 익명으로 패키지를 받는 것은 영향이 없습니다.',
        },
      },
    ],
  },
  {
    version: '1.3.2',
    date: '2026-09-26',
    changes: [
      {
        kind: 'changed',
        text: {
          en: 'Exporting a report now opens a dialog to choose the format and which severities to include, starting from what the page is filtered to. Other filters on the page are listed and can be left out.',
          'zh-TW': '匯出報告時會開啟對話框,可以選擇格式與要包含的嚴重度,預設帶入頁面目前的篩選。頁面上的其他篩選條件會列出來,可以選擇不套用。',
          'zh-CN': '导出报告时会打开对话框,可以选择格式与要包含的严重度,默认带入页面当前的筛选。页面上的其他筛选条件会列出来,可以选择不应用。',
          ja: 'レポートの出力時にダイアログが開き、形式と含める深刻度を選べます。初期値はページの現在の絞り込みです。ページのその他の絞り込みは一覧表示され、適用しないこともできます。',
          ko: '보고서를 내보낼 때 대화 상자가 열려 형식과 포함할 심각도를 고를 수 있습니다. 기본값은 페이지의 현재 필터입니다. 페이지의 다른 필터는 목록으로 보여 주며 적용하지 않을 수도 있습니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'A filtered report showed the severities it left out as 0, which read as "none found". PDF and Excel now mark them as not in the report and say the report is filtered; JSON lists the severities it covers; a CSV, which has nowhere else to say it, carries the filter in its file name.',
          'zh-TW': '套用篩選的報告會把被排除的嚴重度顯示成 0,看起來像是「沒有找到」。現在 PDF 與 Excel 會標示「不在此報告範圍」並註明報告有套用篩選;JSON 會列出涵蓋的嚴重度;CSV 沒有其他地方可以說明,所以把篩選條件寫進檔名。',
          'zh-CN': '应用筛选的报告会把被排除的严重度显示成 0,看起来像是「没有找到」。现在 PDF 与 Excel 会标示「不在此报告范围」并注明报告有应用筛选;JSON 会列出覆盖的严重度;CSV 没有其他地方可以说明,所以把筛选条件写进文件名。',
          ja: '絞り込んだレポートでは、除外した深刻度が 0 と表示され「見つからなかった」ように読めました。PDF と Excel は「このレポートの対象外」と示し絞り込み済みであることを明記、JSON は対象の深刻度を列挙、CSV はファイル名に絞り込み条件を含めます。',
          ko: '필터를 적용한 보고서에서 제외한 심각도가 0으로 표시되어 「발견되지 않음」처럼 읽혔습니다. 이제 PDF와 Excel은 「이 보고서 범위 밖」으로 표시하고 필터 적용을 명시하며, JSON은 포함한 심각도를 나열하고, CSV는 파일 이름에 필터를 담습니다.',
        },
      },
    ],
  },
  {
    version: '1.3.1',
    date: '2026-09-26',
    headline: {
      en: 'Vulnerability findings can be exported as a report — PDF or Excel to read, CSV or JSON for scripts and AI agents.',
      'zh-TW': '漏洞掃描結果可以匯出成報告:PDF、Excel 給人看,CSV、JSON 給程式與 AI agent 使用。',
      'zh-CN': '漏洞扫描结果可以导出成报告:PDF、Excel 给人看,CSV、JSON 给程序与 AI agent 使用。',
      ja: '脆弱性の検出結果をレポートとして出力できるようになりました。PDF・Excel は閲覧用、CSV・JSON はスクリプトや AI エージェント用です。',
      ko: '취약점 결과를 보고서로 내보낼 수 있습니다. PDF·Excel은 사람이 읽기 위한 것, CSV·JSON은 스크립트와 AI 에이전트를 위한 것입니다.',
    },
    changes: [
      {
        kind: 'added',
        text: {
          en: 'Export the Vulnerabilities page as PDF, Excel, CSV or JSON, with the filters it is showing. Each package gets the version to upgrade to that fixes everything listed against it; CSV and JSON carry the purl, the identifier to match a project\'s own dependencies against. PDF and Excel follow the interface language.',
          'zh-TW': '「漏洞」頁面可以依目前的篩選條件匯出成 PDF、Excel、CSV 或 JSON。每個套件都會列出「升級到哪一版可以修掉所有已知漏洞」;CSV 與 JSON 附有 purl,可以直接拿來比對專案自己的依賴。PDF 與 Excel 會跟著介面語言。',
          'zh-CN': '「漏洞」页面可以按当前的筛选条件导出成 PDF、Excel、CSV 或 JSON。每个包都会列出「升级到哪个版本可以修掉所有已知漏洞」;CSV 与 JSON 附有 purl,可以直接用来比对项目自己的依赖。PDF 与 Excel 会跟随界面语言。',
          ja: '「脆弱性」ページを、表示中の絞り込み条件で PDF・Excel・CSV・JSON として出力できます。各パッケージについて、記載された脆弱性をすべて修正するアップグレード先を示します。CSV と JSON には purl が含まれ、プロジェクトの依存関係と照合できます。PDF と Excel は表示言語に従います。',
          ko: '「취약점」 페이지를 현재 필터 그대로 PDF·Excel·CSV·JSON으로 내보낼 수 있습니다. 패키지마다 나열된 취약점을 모두 해결하는 업그레이드 버전을 알려 줍니다. CSV와 JSON에는 프로젝트 의존성과 대조할 수 있는 purl이 들어 있습니다. PDF와 Excel은 화면 언어를 따릅니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'The Kubernetes manifest in deploy/k8s pointed at a locally built image and would not start anywhere else. It now pulls the published release.',
          'zh-TW': 'deploy/k8s 裡的 Kubernetes manifest 指向本機建置的 image,在其他環境無法啟動。現在改為拉取正式發佈的版本。',
          'zh-CN': 'deploy/k8s 里的 Kubernetes manifest 指向本机构建的镜像,在其他环境无法启动。现在改为拉取正式发布的版本。',
          ja: 'deploy/k8s の Kubernetes マニフェストがローカルでビルドしたイメージを指しており、他の環境では起動しませんでした。公開リリースを取得するようになりました。',
          ko: 'deploy/k8s의 Kubernetes 매니페스트가 로컬에서 빌드한 이미지를 가리켜 다른 환경에서는 시작되지 않았습니다. 이제 공개 릴리스를 가져옵니다.',
        },
      },
      {
        kind: 'changed',
        text: {
          en: 'docker compose now pulls the published image instead of building from source, so trying Holiaokho takes one downloaded file and one command. Add --build to run a checkout.',
          'zh-TW': 'docker compose 改為拉取官方 image,不再從原始碼建置,試用好料庫只需要下載一個檔案、執行一行指令。要跑自己的原始碼時加上 --build。',
          'zh-CN': 'docker compose 改为拉取官方镜像,不再从源码构建,试用好料库只需要下载一个文件、执行一行命令。要运行自己的源码时加上 --build。',
          ja: 'docker compose はソースからビルドせず公開イメージを取得するようになり、ファイル 1 つとコマンド 1 行で試せます。手元のソースを使うには --build を付けます。',
          ko: 'docker compose가 소스에서 빌드하지 않고 공개 이미지를 가져오므로, 파일 하나와 명령 한 줄로 Holiaokho를 써 볼 수 있습니다. 직접 받은 소스를 쓰려면 --build를 붙입니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'Column and field labels for a single repository read "Repositories" in English.',
          'zh-TW': '英文介面中,指單一 repository 的欄位名稱顯示成複數的「Repositories」。',
          'zh-CN': '英文界面中,指单个仓库的字段名称显示成复数的「Repositories」。',
          ja: '英語表示で、単一のリポジトリを指す列や項目の名前が複数形の「Repositories」になっていました。',
          ko: '영어 화면에서 하나의 리포지터리를 가리키는 열과 항목 이름이 복수형 「Repositories」로 표시되었습니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'Vulnerabilities in package details now line up, whatever the width of each severity label.',
          'zh-TW': '套件詳情中的漏洞清單現在會對齊,不再因為嚴重度標籤寬度不同而錯位。',
          'zh-CN': '包详情中的漏洞列表现在会对齐,不再因为严重度标签宽度不同而错位。',
          ja: 'パッケージ詳細の脆弱性一覧が、深刻度ラベルの幅に関係なく揃うようになりました。',
          ko: '패키지 상세의 취약점 목록이 심각도 라벨 너비와 관계없이 정렬됩니다.',
        },
      },
    ],
  },
  {
    version: '1.3.0',
    date: '2026-09-26',
    changes: [
      {
        kind: 'added',
        text: {
          en: 'Administrators are told on the dashboard and under the bell when a newer release is out, with a link to what changed. The server asks GitHub once a day, sending nothing but its version; updates.check turns this off.',
          'zh-TW': '有新版本發佈時,管理員會在首頁和鈴鐺看到提示,並附上更新內容的連結。伺服器每天向 GitHub 查詢一次,只送出自己的版本號;可用 updates.check 關閉。',
          'zh-CN': '有新版本发布时,管理员会在首页和铃铛看到提示,并附上更新内容的链接。服务器每天向 GitHub 查询一次,只发送自己的版本号;可用 updates.check 关闭。',
          ja: '新しいリリースが公開されると、管理者はダッシュボードとベルで通知を受け、更新内容へのリンクが表示されます。サーバーは 1 日 1 回 GitHub に問い合わせ、送るのは自身のバージョンだけです。updates.check で無効にできます。',
          ko: '새 릴리스가 나오면 관리자에게 대시보드와 종 아이콘으로 알리고 변경 내용 링크를 보여 줍니다. 서버는 하루 한 번 GitHub에 확인하며 자신의 버전만 보냅니다. updates.check로 끌 수 있습니다.',
        },
      },
    ],
  },
  {
    version: '1.2.0',
    date: '2026-09-26',
    headline: {
      en: 'Stored packages are now checked for known vulnerabilities, and the dashboard says so when any are found.',
      'zh-TW': '現在會檢查已存放的套件是否有已知漏洞,發現時會在首頁提示。',
      'zh-CN': '现在会检查已存储的包是否有已知漏洞,发现时会在首页提示。',
      ja: '保存されているパッケージの既知の脆弱性をチェックし、見つかった場合はダッシュボードに表示するようになりました。',
      ko: '저장된 패키지의 알려진 취약점을 확인하며, 발견되면 대시보드에 표시합니다.',
    },
    changes: [
      {
        kind: 'added',
        text: {
          en: 'Vulnerability scanning against OSV for Maven, npm, PyPI, Go, NuGet, RubyGems, Cargo, Composer, pub and CRAN. The same issue published as a GHSA, a CVE and an ecosystem advisory is counted once. Formats OSV does not cover are shown as not covered, never as clean.',
          'zh-TW': '以 OSV 掃描漏洞,支援 Maven、npm、PyPI、Go、NuGet、RubyGems、Cargo、Composer、pub、CRAN。同一個問題若同時以 GHSA、CVE 與各生態系公告發布,只算一次。OSV 不支援的格式會標示為「未涵蓋」,絕不顯示為「安全」。',
          'zh-CN': '使用 OSV 扫描漏洞,支持 Maven、npm、PyPI、Go、NuGet、RubyGems、Cargo、Composer、pub、CRAN。同一个问题若同时以 GHSA、CVE 与各生态系公告发布,只算一次。OSV 不支持的格式会标示为「未覆盖」,绝不显示为「安全」。',
          ja: 'OSV による脆弱性スキャン(Maven、npm、PyPI、Go、NuGet、RubyGems、Cargo、Composer、pub、CRAN)。GHSA・CVE・各エコシステムで公開された同じ問題は 1 件として数えます。OSV の対象外の形式は「対象外」と表示し、安全とは表示しません。',
          ko: 'OSV를 이용한 취약점 스캔(Maven, npm, PyPI, Go, NuGet, RubyGems, Cargo, Composer, pub, CRAN). GHSA·CVE·생태계 공지로 함께 발표된 같은 문제는 한 건으로 셉니다. OSV 대상이 아닌 형식은 「대상 아님」으로 표시하며 안전하다고 표시하지 않습니다.',
        },
      },
      {
        kind: 'added',
        text: {
          en: 'A Vulnerabilities page, a vulnerabilities section in package details, and a notice on the dashboard coloured by the worst finding.',
          'zh-TW': '新增「漏洞」頁面、套件詳情中的漏洞區塊,以及首頁上依最嚴重等級上色的提示。',
          'zh-CN': '新增「漏洞」页面、包详情中的漏洞区块,以及首页上按最严重等级着色的提示。',
          ja: '「脆弱性」ページ、パッケージ詳細の脆弱性セクション、最も深刻な検出に応じて色が変わるダッシュボードの通知を追加しました。',
          ko: '「취약점」 페이지, 패키지 상세의 취약점 섹션, 가장 심각한 항목에 따라 색이 바뀌는 대시보드 알림을 추가했습니다.',
        },
      },
      {
        kind: 'added',
        text: {
          en: 'Newly found critical and high vulnerabilities are sent once by email and as a vulnerability.found webhook. The threshold is vulnerabilities.notify_min_severity.',
          'zh-TW': '新發現的 Critical 與 High 漏洞會以 email 及 vulnerability.found webhook 通知一次。門檻由 vulnerabilities.notify_min_severity 設定。',
          'zh-CN': '新发现的 Critical 与 High 漏洞会以 email 及 vulnerability.found webhook 通知一次。阈值由 vulnerabilities.notify_min_severity 设置。',
          ja: '新たに見つかった緊急・高の脆弱性は、メールと vulnerability.found Webhook で一度だけ通知されます。しきい値は vulnerabilities.notify_min_severity で設定します。',
          ko: '새로 발견된 심각·높음 취약점은 이메일과 vulnerability.found 웹훅으로 한 번 알립니다. 기준은 vulnerabilities.notify_min_severity로 설정합니다.',
        },
      },
      {
        kind: 'added',
        text: {
          en: 'Scanning is on by default and each repository can opt out in its settings. A server that cannot reach OSV keeps working and says so on the Vulnerabilities page; osv_url can point at a mirror.',
          'zh-TW': '掃描預設開啟,每個 repository 都可以在設定中關閉。無法連線到 OSV 的伺服器會照常運作,並在「漏洞」頁面顯示;osv_url 可以指向鏡像站。',
          'zh-CN': '扫描默认开启,每个 repository 都可以在设置中关闭。无法连接到 OSV 的服务器会照常运行,并在「漏洞」页面显示;osv_url 可以指向镜像站。',
          ja: 'スキャンは既定で有効で、リポジトリごとに設定で無効にできます。OSV に接続できないサーバーも通常どおり動作し、「脆弱性」ページにその旨を表示します。osv_url でミラーを指定できます。',
          ko: '스캔은 기본으로 켜져 있으며 리포지터리마다 설정에서 끌 수 있습니다. OSV에 연결할 수 없는 서버도 정상 동작하며 「취약점」 페이지에 표시합니다. osv_url로 미러를 지정할 수 있습니다.',
        },
      },
    ],
  },
  {
    version: '1.1.3',
    date: '2026-09-24',
    changes: [
      {
        kind: 'added',
        text: {
          en: 'A "Delete incomplete uploads" task removes uploads a client started and never finished — an interrupted docker push, for example — once they have gone untouched for a day. They used to stay on disk for good.',
          'zh-TW': '新增「刪除未完成的上傳」任務:client 開始上傳卻沒完成的檔案(例如中斷的 docker push),超過一天沒更新就會刪除。以前這些檔案會永遠留在硬碟上。',
          'zh-CN': '新增「删除未完成的上传」任务:客户端开始上传却没完成的文件(例如中断的 docker push),超过一天没更新就会删除。以前这些文件会永远留在硬盘上。',
          ja: '「未完了アップロードの削除」タスクを追加しました。中断された docker push など、開始されたまま完了しなかったアップロードを 1 日更新がなければ削除します。以前はディスクに残り続けていました。',
          ko: '「미완료 업로드 삭제」 작업을 추가했습니다. 중단된 docker push처럼 시작만 하고 끝나지 않은 업로드를 하루 동안 변경이 없으면 삭제합니다. 이전에는 디스크에 계속 남아 있었습니다.',
        },
      },
      {
        kind: 'added',
        text: {
          en: 'A "Delete temporary files" task removes what the server leaves behind when it stops in the middle of a write. On S3 that includes unfinished multipart uploads, which S3 keeps and bills for until they are aborted. Only this server\'s own files are touched, so a shared bucket is safe.',
          'zh-TW': '新增「刪除暫存檔」任務:清掉伺服器在寫入途中停止時留下的檔案。在 S3 上也包括沒完成的 multipart upload,這種殘留 S3 會一直保留而且持續計費,直到被中止為止。只會動到本伺服器自己的檔案,共用的 bucket 也安全。',
          'zh-CN': '新增「删除临时文件」任务:清掉服务器在写入途中停止时留下的文件。在 S3 上也包括没完成的 multipart upload,这种残留 S3 会一直保留而且持续计费,直到被中止为止。只会动到本服务器自己的文件,共用的 bucket 也安全。',
          ja: '「一時ファイルの削除」タスクを追加しました。書き込み途中でサーバーが停止したときに残るファイルを削除します。S3 では、中止されるまで保持・課金され続ける未完了のマルチパートアップロードも対象です。このサーバー自身のファイルだけを扱うため、共有バケットでも安全です。',
          ko: '「임시 파일 삭제」 작업을 추가했습니다. 쓰기 도중 서버가 멈췄을 때 남는 파일을 삭제합니다. S3에서는 중단하기 전까지 보관되며 요금이 계속 청구되는 미완료 멀티파트 업로드도 포함됩니다. 이 서버의 파일만 다루므로 공유 버킷에서도 안전합니다.',
        },
      },
    ],
  },
  {
    version: '1.1.2',
    date: '2026-09-23',
    headline: {
      en: 'Pulling through a slow upstream now starts right away, and a mistyped image name no longer takes a whole registry offline.',
      'zh-TW': '透過緩慢的上游拉取時會立刻開始傳輸;打錯 image 名稱也不會再讓整個 registry 停擺。',
      'zh-CN': '通过缓慢的上游拉取时会立刻开始传输;打错镜像名称也不会再让整个 registry 停摆。',
      ja: '遅い上流経由のプルがすぐに始まるようになり、イメージ名の打ち間違いでレジストリ全体が止まることもなくなりました。',
      ko: '느린 업스트림을 통한 풀이 바로 시작되며, 이미지 이름 오타로 레지스트리 전체가 멈추는 일도 없어졌습니다.',
    },
    changes: [
      {
        kind: 'fixed',
        text: {
          en: 'Docker layers are sent to the client as they arrive from upstream, instead of after the whole layer has been downloaded. Over a slow link a large layer used to produce minutes of silence, long enough for the client or a proxy in between to give up. Several clients pulling the same layer share one download.',
          'zh-TW': 'Docker layer 會邊從上游收到邊轉給 client,不再等整個 layer 下載完才開始送。以前在慢速連線下,大的 layer 會沉默好幾分鐘,久到 client 或中間的 proxy 直接放棄。多個 client 同時拉同一個 layer 時共用同一個下載。',
          'zh-CN': 'Docker layer 会边从上游收到边转给客户端,不再等整个 layer 下载完才开始发送。以前在慢速连接下,大的 layer 会沉默好几分钟,久到客户端或中间的代理直接放弃。多个客户端同时拉同一个 layer 时共用同一个下载。',
          ja: 'Docker レイヤーは上流から届いた分からクライアントへ送られるようになりました。以前は遅い回線だと大きなレイヤーで数分間なにも返らず、クライアントや中継プロキシが諦めてしまうことがありました。同じレイヤーを取得する複数のクライアントは一つのダウンロードを共有します。',
          ko: 'Docker 레이어가 업스트림에서 도착하는 대로 클라이언트에 전달됩니다. 이전에는 느린 회선에서 큰 레이어가 몇 분간 아무 응답이 없어 클라이언트나 중간 프록시가 포기하곤 했습니다. 같은 레이어를 받는 여러 클라이언트는 하나의 다운로드를 공유합니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'A slow layer download is no longer cut off at ten minutes. It is abandoned only after a minute with no data at all, so a large layer over a slow link finishes and is cached instead of failing at the same point on every attempt.',
          'zh-TW': '慢速的 layer 下載不再在十分鐘時被切斷,只有整整一分鐘沒收到任何資料才會放棄。慢速連線下的大 layer 因此能下載完並進快取,而不是每次都在同一個地方失敗。',
          'zh-CN': '慢速的 layer 下载不再在十分钟时被切断,只有整整一分钟没收到任何数据才会放弃。慢速连接下的大 layer 因此能下载完并进缓存,而不是每次都在同一个地方失败。',
          ja: '遅いレイヤーのダウンロードが 10 分で打ち切られなくなりました。1 分間まったくデータが来ない場合にのみ中断するため、遅い回線でも大きなレイヤーが最後まで取得されキャッシュされます。',
          ko: '느린 레이어 다운로드가 10분에서 끊기지 않습니다. 1분 동안 데이터가 전혀 오지 않을 때만 중단하므로, 느린 회선에서도 큰 레이어가 끝까지 받아져 캐시됩니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'Asking a registry such as GHCR for an image that does not exist no longer blocks that proxy for 30 seconds. The refusal was being counted as the upstream being down, so one typo made every image on it unavailable to everyone.',
          'zh-TW': '向 GHCR 等 registry 要一個不存在的 image,不會再讓該 proxy 被封鎖 30 秒。以前這種拒絕會被當成上游故障,一次打錯字就讓所有人都拉不到那個 registry 上的任何 image。',
          'zh-CN': '向 GHCR 等 registry 要一个不存在的镜像,不会再让该代理被封锁 30 秒。以前这种拒绝会被当成上游故障,一次打错字就让所有人都拉不到那个 registry 上的任何镜像。',
          ja: 'GHCR などに存在しないイメージを要求しても、そのプロキシが 30 秒間ブロックされなくなりました。拒否が上流障害として数えられていたため、一度の打ち間違いで全員がそのレジストリのイメージを取得できなくなっていました。',
          ko: 'GHCR 등에 존재하지 않는 이미지를 요청해도 해당 프록시가 30초간 차단되지 않습니다. 거부 응답이 업스트림 장애로 집계되어, 오타 한 번에 모두가 그 레지스트리의 이미지를 받을 수 없었습니다.',
        },
      },
      {
        kind: 'fixed',
        text: {
          en: 'While an upstream is blocked, requests to it report the upstream as unavailable instead of saying the image does not exist.',
          'zh-TW': '上游被封鎖期間,請求會回報「上游無法使用」,而不是說 image 不存在。',
          'zh-CN': '上游被封锁期间,请求会报告「上游不可用」,而不是说镜像不存在。',
          ja: '上流がブロックされている間は、イメージが存在しないではなく上流が利用できないと応答します。',
          ko: '업스트림이 차단된 동안에는 이미지가 없다고 하지 않고 업스트림을 사용할 수 없다고 응답합니다.',
        },
      },
    ],
  },
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
