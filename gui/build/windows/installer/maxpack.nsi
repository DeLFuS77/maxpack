; SPDX-License-Identifier: GPL-3.0-or-later
;
; MaxPack's installer for Windows. It is built with makensis and these definitions:
;   VERSION  (for example dev-1a2b3c4 or 1.2.3)    NUMBERS  (4 numbers: 1.2.3.0)
;   PROGRAM  (MaxPack.exe)   CORE (maxpack-core.exe)   PAR2 (maxpack-par2.exe)   LICENSE (LICENSE)
;   THUMBS (maxpack-thumbs.dll, the thumbnail provider of .p5z files)   NOTICES (THIRD_PARTY_NOTICES)
;   ICON (appicon.ico)
;   OUTFILE  (the installer that comes out)
;
; Installs for the user, in %LOCALAPPDATA%\Programs\MaxPack, without administrator rights. It does not carry the
; "settings" folder (that is what makes the zip portable): MaxPack keeps its own in %APPDATA%.
; An already installed MaxPack is updated in its folder.
;
; It offers MaxPack's places in the Windows Explorer, each off until it is ticked: the right-click menu, Send to and
; opening .p5z files. MaxPack itself adds them, as its Settings do (MaxPack.exe --installer add, see gui/shell.go),
; for this user only; the uninstaller has MaxPack take them all away (--installer remove), or takes them away itself
; when MaxPack cannot run. Silently, /MENU, /SENDTO and /OPENP5Z tick them (before /D=, which goes last).

Unicode true
ManifestDPIAware true
RequestExecutionLevel user
SetCompressor /SOLID lzma

!include MUI2.nsh
!include LogicLib.nsh
!include nsDialogs.nsh
!include FileFunc.nsh

!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\MaxPack"
; the classes of this user, where MaxPack writes its places in the Explorer (gui/internal/explorer)
!define CLASSES "Software\Classes"

Name "MaxPack"
Caption "MaxPack ${VERSION}"
BrandingText "MaxPack ${VERSION}"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\MaxPack"
InstallDirRegKey HKCU "${UNINSTKEY}" "InstallLocation"
ShowInstDetails show
ShowUninstDetails show

VIProductVersion "${NUMBERS}"
VIFileVersion "${NUMBERS}"
VIAddVersionKey /LANG=0 "ProductName" "MaxPack"
VIAddVersionKey /LANG=0 "FileDescription" "MaxPack installer"
VIAddVersionKey /LANG=0 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=0 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=0 "LegalCopyright" "GPL-3.0-or-later"

!define MUI_ICON "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_ABORTWARNING

; the places in the Explorer chosen (1, ticked) and their boxes in the page
Var MenuState
Var SendToState
Var FilesState
Var MenuBox
Var SendToBox
Var FilesBox

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${LICENSE}"
!insertmacro MUI_PAGE_DIRECTORY
Page custom ExplorerPage ExplorerLeave
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\MaxPack.exe"
; nothing MaxPack installs needs a restart: the finish page never offers one
!define MUI_FINISHPAGE_NOREBOOTSUPPORT
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; The languages of the window, the same eleven. The installer takes the one of Windows when it has it; English is
; first, so it is the one used when Windows' is none of them. The texts that are not of the standard pages (the
; places in the Explorer, and KeepSettings) have their own translation.
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "Russian"
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "Spanish"
!insertmacro MUI_LANGUAGE "French"
!insertmacro MUI_LANGUAGE "Japanese"
!insertmacro MUI_LANGUAGE "PortugueseBR"
!insertmacro MUI_LANGUAGE "Italian"
!insertmacro MUI_LANGUAGE "Korean"
!insertmacro MUI_LANGUAGE "Arabic"

LangString KeepSettings ${LANG_ENGLISH} "Also delete the settings of MaxPack and what its window remembers?"
LangString KeepSettings ${LANG_RUSSIAN} "Также удалить настройки MaxPack и то, что запоминает его окно?"
LangString KeepSettings ${LANG_SIMPCHINESE} "是否同时删除 MaxPack 的设置以及它的窗口记住的内容？"
LangString KeepSettings ${LANG_GERMAN} "Auch die Einstellungen von MaxPack und das, was sich sein Fenster merkt, löschen?"
LangString KeepSettings ${LANG_SPANISH} "¿Borrar también los ajustes de MaxPack y lo que recuerda de la ventana?"
LangString KeepSettings ${LANG_FRENCH} "Supprimer aussi les paramètres de MaxPack et ce dont sa fenêtre se souvient ?"
LangString KeepSettings ${LANG_JAPANESE} "MaxPack の設定とウィンドウが記憶している内容も削除しますか？"
LangString KeepSettings ${LANG_PORTUGUESEBR} "Apagar também as configurações do MaxPack e o que a janela dele lembra?"
LangString KeepSettings ${LANG_ITALIAN} "Eliminare anche le impostazioni di MaxPack e ciò che la sua finestra ricorda?"
LangString KeepSettings ${LANG_KOREAN} "MaxPack의 설정과 창이 기억하는 내용도 함께 삭제할까요?"
LangString KeepSettings ${LANG_ARABIC} "هل تريد أيضًا حذف إعدادات MaxPack وما تتذكره نافذته؟"

LangString ExplorerTitle ${LANG_ENGLISH} "Windows Explorer"
LangString ExplorerTitle ${LANG_RUSSIAN} "Проводник Windows"
LangString ExplorerTitle ${LANG_SIMPCHINESE} "Windows 资源管理器"
LangString ExplorerTitle ${LANG_GERMAN} "Windows-Explorer"
LangString ExplorerTitle ${LANG_SPANISH} "Explorador de Windows"
LangString ExplorerTitle ${LANG_FRENCH} "Explorateur Windows"
LangString ExplorerTitle ${LANG_JAPANESE} "Windows エクスプローラー"
LangString ExplorerTitle ${LANG_PORTUGUESEBR} "Explorador de Arquivos do Windows"
LangString ExplorerTitle ${LANG_ITALIAN} "Esplora file di Windows"
LangString ExplorerTitle ${LANG_KOREAN} "Windows 파일 탐색기"
LangString ExplorerTitle ${LANG_ARABIC} "مستكشف ملفات Windows"

LangString ExplorerSub ${LANG_ENGLISH} "Choose what MaxPack adds to the Explorer, for you only. Settings changes it later."
LangString ExplorerSub ${LANG_RUSSIAN} "Выберите, что MaxPack добавит в Проводник, только для вас. Позже это меняется в настройках."
LangString ExplorerSub ${LANG_SIMPCHINESE} "选择 MaxPack 要添加到资源管理器的内容，只对你生效。以后可在设置中更改。"
LangString ExplorerSub ${LANG_GERMAN} "Wählen Sie, was MaxPack dem Explorer hinzufügt, nur für Sie. Die Einstellungen ändern es später."
LangString ExplorerSub ${LANG_SPANISH} "Elige qué añade MaxPack al Explorador, solo para ti. Luego se cambia en Ajustes."
LangString ExplorerSub ${LANG_FRENCH} "Choisissez ce que MaxPack ajoute à l'Explorateur, pour vous seul. Les Paramètres le changent ensuite."
LangString ExplorerSub ${LANG_JAPANESE} "MaxPack がエクスプローラーに追加するものを選んでください。あなただけに適用され、後で設定から変更できます。"
LangString ExplorerSub ${LANG_PORTUGUESEBR} "Escolha o que o MaxPack adiciona ao Explorador, só para você. Depois isso muda nas Configurações."
LangString ExplorerSub ${LANG_ITALIAN} "Scegli cosa aggiunge MaxPack a Esplora file, solo per te. Si cambia poi nelle Impostazioni."
LangString ExplorerSub ${LANG_KOREAN} "MaxPack이 파일 탐색기에 추가할 항목을 고르세요. 현재 사용자에게만 적용되며, 나중에 설정에서 바꿀 수 있습니다."
LangString ExplorerSub ${LANG_ARABIC} "اختر ما يضيفه MaxPack إلى المستكشف، لك وحدك. يمكن تغييره لاحقًا من الإعدادات."

LangString OptMenu ${LANG_ENGLISH} "Right-click menu"
LangString OptMenu ${LANG_RUSSIAN} "Меню правой кнопки"
LangString OptMenu ${LANG_SIMPCHINESE} "右键菜单"
LangString OptMenu ${LANG_GERMAN} "Rechtsklick-Menü"
LangString OptMenu ${LANG_SPANISH} "Menú del clic derecho"
LangString OptMenu ${LANG_FRENCH} "Menu du clic droit"
LangString OptMenu ${LANG_JAPANESE} "右クリックメニュー"
LangString OptMenu ${LANG_PORTUGUESEBR} "Menu do clique direito"
LangString OptMenu ${LANG_ITALIAN} "Menu del clic destro"
LangString OptMenu ${LANG_KOREAN} "오른쪽 클릭 메뉴"
LangString OptMenu ${LANG_ARABIC} "قائمة النقر بزر الماوس الأيمن"

LangString OptMenuHint ${LANG_ENGLISH} "Compress with MaxPack on folders, game images and PS5 packages; Verify with MaxPack and Details on .p5z files."
LangString OptMenuHint ${LANG_RUSSIAN} "«Сжать с помощью MaxPack» на папках, образах игр и пакетах PS5; «Проверить с помощью MaxPack» и «Сведения» на файлах .p5z."
LangString OptMenuHint ${LANG_SIMPCHINESE} "文件夹、游戏镜像和 PS5 安装包上的“使用 MaxPack 压缩”；.p5z 文件上的“使用 MaxPack 校验”和“详情”。"
LangString OptMenuHint ${LANG_GERMAN} "„Mit MaxPack komprimieren“ auf Ordnern, Spiel-Images und PS5-Paketen; „Mit MaxPack überprüfen“ und „Details“ auf .p5z-Dateien."
LangString OptMenuHint ${LANG_SPANISH} "Comprimir con MaxPack en carpetas, imágenes de juegos y paquetes de PS5; Verificar con MaxPack y Detalles en los .p5z."
LangString OptMenuHint ${LANG_FRENCH} "« Compresser avec MaxPack » sur les dossiers, images de jeux et paquets PS5 ; « Vérifier avec MaxPack » et « Détails » sur les .p5z."
LangString OptMenuHint ${LANG_JAPANESE} "フォルダー、ゲームのイメージ、PS5 パッケージに「MaxPack で圧縮」、.p5z ファイルに「MaxPack で検証」と「詳細」。"
LangString OptMenuHint ${LANG_PORTUGUESEBR} "Comprimir com o MaxPack em pastas, imagens de jogos e pacotes de PS5; Verificar com o MaxPack e Detalhes em arquivos .p5z."
LangString OptMenuHint ${LANG_ITALIAN} "Comprimi con MaxPack su cartelle, immagini di giochi e pacchetti PS5; Verifica con MaxPack e Dettagli sui file .p5z."
LangString OptMenuHint ${LANG_KOREAN} "폴더, 게임 이미지, PS5 패키지에 ‘MaxPack으로 압축’, .p5z 파일에 ‘MaxPack으로 검증’과 ‘세부 정보’."
LangString OptMenuHint ${LANG_ARABIC} "«ضغط باستخدام MaxPack» على المجلدات وصور الألعاب وحزم PS5؛ «تحقق باستخدام MaxPack» و«التفاصيل» على ملفات ‎.p5z‎."

LangString OptSendTo ${LANG_ENGLISH} "Send to › MaxPack"
LangString OptSendTo ${LANG_RUSSIAN} "Отправить › MaxPack"
LangString OptSendTo ${LANG_SIMPCHINESE} "发送到 › MaxPack"
LangString OptSendTo ${LANG_GERMAN} "Senden an › MaxPack"
LangString OptSendTo ${LANG_SPANISH} "Enviar a › MaxPack"
LangString OptSendTo ${LANG_FRENCH} "Envoyer vers › MaxPack"
LangString OptSendTo ${LANG_JAPANESE} "送る › MaxPack"
LangString OptSendTo ${LANG_PORTUGUESEBR} "Enviar para › MaxPack"
LangString OptSendTo ${LANG_ITALIAN} "Invia a › MaxPack"
LangString OptSendTo ${LANG_KOREAN} "보내기 › MaxPack"
LangString OptSendTo ${LANG_ARABIC} "إرسال إلى › MaxPack"

LangString OptSendToHint ${LANG_ENGLISH} "MaxPack in Send to, to give it several games at once."
LangString OptSendToHint ${LANG_RUSSIAN} "MaxPack в меню «Отправить», чтобы передать ему сразу несколько игр."
LangString OptSendToHint ${LANG_SIMPCHINESE} "在“发送到”中加入 MaxPack，一次交给它多个游戏。"
LangString OptSendToHint ${LANG_GERMAN} "MaxPack in „Senden an“, um ihm mehrere Spiele auf einmal zu geben."
LangString OptSendToHint ${LANG_SPANISH} "MaxPack en Enviar a, para darle varios juegos a la vez."
LangString OptSendToHint ${LANG_FRENCH} "MaxPack dans « Envoyer vers », pour lui donner plusieurs jeux à la fois."
LangString OptSendToHint ${LANG_JAPANESE} "「送る」に MaxPack を加え、複数のゲームを一度に渡せます。"
LangString OptSendToHint ${LANG_PORTUGUESEBR} "O MaxPack em Enviar para, para entregar a ele vários jogos de uma vez."
LangString OptSendToHint ${LANG_ITALIAN} "MaxPack in Invia a, per dargli più giochi alla volta."
LangString OptSendToHint ${LANG_KOREAN} "‘보내기’에 MaxPack을 넣어 여러 게임을 한 번에 넘길 수 있습니다."
LangString OptSendToHint ${LANG_ARABIC} "MaxPack في «إرسال إلى»، لتعطيه عدة ألعاب دفعة واحدة."

LangString OptFiles ${LANG_ENGLISH} "Open .p5z files with MaxPack"
LangString OptFiles ${LANG_RUSSIAN} "Открывать файлы .p5z в MaxPack"
LangString OptFiles ${LANG_SIMPCHINESE} "用 MaxPack 打开 .p5z 文件"
LangString OptFiles ${LANG_GERMAN} ".p5z-Dateien mit MaxPack öffnen"
LangString OptFiles ${LANG_SPANISH} "Abrir los .p5z con MaxPack"
LangString OptFiles ${LANG_FRENCH} "Ouvrir les fichiers .p5z avec MaxPack"
LangString OptFiles ${LANG_JAPANESE} ".p5z ファイルを MaxPack で開く"
LangString OptFiles ${LANG_PORTUGUESEBR} "Abrir arquivos .p5z com o MaxPack"
LangString OptFiles ${LANG_ITALIAN} "Apri i file .p5z con MaxPack"
LangString OptFiles ${LANG_KOREAN} ".p5z 파일을 MaxPack으로 열기"
LangString OptFiles ${LANG_ARABIC} "فتح ملفات ‎.p5z‎ باستخدام MaxPack"

LangString OptFilesHint ${LANG_ENGLISH} "A double click on a .p5z shows its details, and the Explorer shows its game's cover. A program that already opens them keeps them."
LangString OptFilesHint ${LANG_RUSSIAN} "Двойной щелчок по .p5z показывает его сведения, а Проводник показывает обложку его игры. Программа, которая уже их открывает, остаётся."
LangString OptFilesHint ${LANG_SIMPCHINESE} "双击 .p5z 会显示它的详情，资源管理器会显示其游戏的封面。已经打开它们的程序会保留。"
LangString OptFilesHint ${LANG_GERMAN} "Ein Doppelklick auf eine .p5z-Datei zeigt ihre Details, und der Explorer zeigt das Cover ihres Spiels. Ein Programm, das sie schon öffnet, behält sie."
LangString OptFilesHint ${LANG_SPANISH} "Un doble clic en un .p5z muestra sus detalles, y el Explorador muestra la portada de su juego. Un programa que ya los abre los conserva."
LangString OptFilesHint ${LANG_FRENCH} "Un double clic sur un .p5z affiche ses détails, et l'Explorateur montre la jaquette de son jeu. Un programme qui les ouvre déjà les garde."
LangString OptFilesHint ${LANG_JAPANESE} ".p5z をダブルクリックすると詳細が表示され、エクスプローラーにはそのゲームのカバーが表示されます。すでに開いているプログラムはそのままです。"
LangString OptFilesHint ${LANG_PORTUGUESEBR} "Um clique duplo em um .p5z mostra os detalhes, e o Explorador de Arquivos mostra a capa do jogo. Um programa que já os abre continua com eles."
LangString OptFilesHint ${LANG_ITALIAN} "Un doppio clic su un .p5z ne mostra i dettagli, ed Esplora file mostra la copertina del suo gioco. Un programma che già li apre li tiene."
LangString OptFilesHint ${LANG_KOREAN} ".p5z를 두 번 클릭하면 세부 정보가 나타나고, 파일 탐색기에 그 게임의 표지가 표시됩니다. 이미 여는 프로그램은 그대로 유지됩니다."
LangString OptFilesHint ${LANG_ARABIC} "النقر المزدوج على ملف ‎.p5z‎ يعرض تفاصيله، ويعرض مستكشف الملفات غلاف لعبته. البرنامج الذي يفتحها الآن يبقى كما هو."

LangString ExplorerFailed ${LANG_ENGLISH} "MaxPack could not add itself to the Explorer: turn it on in Settings › Windows Explorer."
LangString ExplorerFailed ${LANG_RUSSIAN} "MaxPack не смог добавить себя в Проводник: включите это в настройках, раздел «Проводник Windows»."
LangString ExplorerFailed ${LANG_SIMPCHINESE} "MaxPack 无法将自己添加到资源管理器：请在设置 › Windows 资源管理器中打开。"
LangString ExplorerFailed ${LANG_GERMAN} "MaxPack konnte sich nicht zum Explorer hinzufügen: Schalten Sie es unter Einstellungen › Windows-Explorer ein."
LangString ExplorerFailed ${LANG_SPANISH} "MaxPack no pudo añadirse al Explorador: actívalo en Ajustes › Explorador de Windows."
LangString ExplorerFailed ${LANG_FRENCH} "MaxPack n'a pas pu s'ajouter à l'Explorateur : activez-le dans Paramètres › Explorateur Windows."
LangString ExplorerFailed ${LANG_JAPANESE} "MaxPack をエクスプローラーに追加できませんでした。設定 › Windows エクスプローラーでオンにしてください。"
LangString ExplorerFailed ${LANG_PORTUGUESEBR} "O MaxPack não conseguiu se adicionar ao Explorador: ative em Configurações › Explorador de Arquivos do Windows."
LangString ExplorerFailed ${LANG_ITALIAN} "MaxPack non è riuscito ad aggiungersi a Esplora file: attivalo in Impostazioni › Esplora file di Windows."
LangString ExplorerFailed ${LANG_KOREAN} "MaxPack을 파일 탐색기에 추가하지 못했습니다. 설정 › Windows 파일 탐색기에서 켜세요."
LangString ExplorerFailed ${LANG_ARABIC} "تعذر على MaxPack إضافة نفسه إلى المستكشف: فعّله من الإعدادات › مستكشف ملفات Windows."
; the code of the language of the installer, as the window names it (frontend/i18n), in $1
Function LangCode
  StrCpy $1 "en"
  ${If} $LANGUAGE = ${LANG_RUSSIAN}
    StrCpy $1 "ru"
  ${ElseIf} $LANGUAGE = ${LANG_SIMPCHINESE}
    StrCpy $1 "zh"
  ${ElseIf} $LANGUAGE = ${LANG_GERMAN}
    StrCpy $1 "de"
  ${ElseIf} $LANGUAGE = ${LANG_SPANISH}
    StrCpy $1 "es"
  ${ElseIf} $LANGUAGE = ${LANG_FRENCH}
    StrCpy $1 "fr"
  ${ElseIf} $LANGUAGE = ${LANG_JAPANESE}
    StrCpy $1 "ja"
  ${ElseIf} $LANGUAGE = ${LANG_PORTUGUESEBR}
    StrCpy $1 "pt"
  ${ElseIf} $LANGUAGE = ${LANG_ITALIAN}
    StrCpy $1 "it"
  ${ElseIf} $LANGUAGE = ${LANG_KOREAN}
    StrCpy $1 "ko"
  ${ElseIf} $LANGUAGE = ${LANG_ARABIC}
    StrCpy $1 "ar"
  ${EndIf}
FunctionEnd

; nothing is ticked unless the command line says so (a silent install: /MENU, /SENDTO, /OPENP5Z)
Function .onInit
  StrCpy $MenuState ${BST_UNCHECKED}
  StrCpy $SendToState ${BST_UNCHECKED}
  StrCpy $FilesState ${BST_UNCHECKED}
  ${GetParameters} $R0
  ClearErrors
  ${GetOptions} $R0 "/MENU" $R1
  ${IfNot} ${Errors}
    StrCpy $MenuState ${BST_CHECKED}
  ${EndIf}
  ClearErrors
  ${GetOptions} $R0 "/SENDTO" $R1
  ${IfNot} ${Errors}
    StrCpy $SendToState ${BST_CHECKED}
  ${EndIf}
  ClearErrors
  ${GetOptions} $R0 "/OPENP5Z" $R1
  ${IfNot} ${Errors}
    StrCpy $FilesState ${BST_CHECKED}
  ${EndIf}
FunctionEnd

; the places in the Explorer, each off until it is ticked
Function ExplorerPage
  !insertmacro MUI_HEADER_TEXT "$(ExplorerTitle)" "$(ExplorerSub)"
  nsDialogs::Create 1018
  Pop $0
  ${If} $0 == error
    Abort
  ${EndIf}
  ${NSD_CreateCheckbox} 0u 0u 300u 10u "$(OptMenu)"
  Pop $MenuBox
  ${NSD_SetState} $MenuBox $MenuState
  ${NSD_CreateLabel} 12u 12u 288u 20u "$(OptMenuHint)"
  Pop $0
  ${NSD_CreateCheckbox} 0u 40u 300u 10u "$(OptSendTo)"
  Pop $SendToBox
  ${NSD_SetState} $SendToBox $SendToState
  ${NSD_CreateLabel} 12u 52u 288u 20u "$(OptSendToHint)"
  Pop $0
  ${NSD_CreateCheckbox} 0u 80u 300u 10u "$(OptFiles)"
  Pop $FilesBox
  ${NSD_SetState} $FilesBox $FilesState
  ${NSD_CreateLabel} 12u 92u 288u 28u "$(OptFilesHint)"
  Pop $0
  nsDialogs::Show
FunctionEnd

Function ExplorerLeave
  ${NSD_GetState} $MenuBox $MenuState
  ${NSD_GetState} $SendToBox $SendToState
  ${NSD_GetState} $FilesBox $FilesState
FunctionEnd

; what MaxPack writes in the Explorer (gui/internal/explorer), taken away by the uninstaller itself when MaxPack
; cannot run: the keys and values that are MaxPack's (the thumbnail provider of .p5z files too), and the default
; of .p5z only while it is MaxPack's
Function un.RemoveExplorer
  DeleteRegKey HKCU "${CLASSES}\Directory\shell\MaxPack.Compress"
  DeleteRegKey HKCU "${CLASSES}\SystemFileAssociations\.exfat\shell\MaxPack.Compress"
  DeleteRegKey HKCU "${CLASSES}\SystemFileAssociations\.ffpkg\shell\MaxPack.Compress"
  DeleteRegKey HKCU "${CLASSES}\SystemFileAssociations\.ffpfs\shell\MaxPack.Compress"
  DeleteRegKey HKCU "${CLASSES}\SystemFileAssociations\.ffpfsc\shell\MaxPack.Compress"
  DeleteRegKey HKCU "${CLASSES}\SystemFileAssociations\.pkg\shell\MaxPack.Compress"
  DeleteRegKey HKCU "${CLASSES}\SystemFileAssociations\.p5z\shell\MaxPack.1Verify"
  DeleteRegKey HKCU "${CLASSES}\SystemFileAssociations\.p5z\shell\MaxPack.2Details"
  DeleteRegValue HKCU "${CLASSES}\.p5z\OpenWithProgids" "MaxPack.p5z"
  DeleteRegKey /ifempty HKCU "${CLASSES}\.p5z\OpenWithProgids"
  ReadRegStr $1 HKCU "${CLASSES}\.p5z" ""
  ${If} $1 == "MaxPack.p5z"
    DeleteRegValue HKCU "${CLASSES}\.p5z" ""
  ${EndIf}
  DeleteRegKey /ifempty HKCU "${CLASSES}\.p5z"
  DeleteRegKey HKCU "${CLASSES}\MaxPack.p5z"
  DeleteRegValue HKCU "Software\RegisteredApplications" "MaxPack"
  DeleteRegKey HKCU "Software\MaxPack\Capabilities"
  DeleteRegKey /ifempty HKCU "Software\MaxPack"
  ; the thumbnail handler of .p5z only while it is MaxPack's (another program may have put its own)
  ReadRegStr $1 HKCU "${CLASSES}\SystemFileAssociations\.p5z\ShellEx\{e357fccd-a995-4576-b01f-234630154e96}" ""
  ${If} $1 == "{266D6364-17C0-4B3E-BB33-7EEB90C91837}"
    DeleteRegValue HKCU "${CLASSES}\SystemFileAssociations\.p5z\ShellEx\{e357fccd-a995-4576-b01f-234630154e96}" ""
    DeleteRegKey /ifempty HKCU "${CLASSES}\SystemFileAssociations\.p5z\ShellEx\{e357fccd-a995-4576-b01f-234630154e96}"
    DeleteRegKey /ifempty HKCU "${CLASSES}\SystemFileAssociations\.p5z\ShellEx"
  ${EndIf}
  DeleteRegKey HKCU "${CLASSES}\CLSID\{266D6364-17C0-4B3E-BB33-7EEB90C91837}"
  Delete "$SENDTO\MaxPack.lnk"
  ; the Explorer shows the programs of the files as they are now
  System::Call "shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)"
FunctionEnd

Section "MaxPack"
  SetOutPath "$INSTDIR"
  ; the window looks for the engine, and for maxpack-par2 (the repair data of a .p5z), next to it
  File "/oname=MaxPack.exe" "${PROGRAM}"
  File "/oname=maxpack-core.exe" "${CORE}"
  File "/oname=maxpack-par2.exe" "${PAR2}"
  ; the thumbnail provider of .p5z files, which the Explorer (dllhost) may have loaded while it is updated: a
  ; loaded DLL can be renamed but not overwritten, so it is set aside, and goes now if it can, else at the next
  ; update or uninstall. Never left for a restart: that would make the finish page ask for one
  Delete "$INSTDIR\maxpack-thumbs-*.old"
  Delete "$INSTDIR\maxpack-thumbs.dll"
  ${If} ${FileExists} "$INSTDIR\maxpack-thumbs.dll"
    System::Call "kernel32::GetTickCount() i .r0"
    Rename "$INSTDIR\maxpack-thumbs.dll" "$INSTDIR\maxpack-thumbs-$0.old"
    Delete "$INSTDIR\maxpack-thumbs-$0.old"
  ${EndIf}
  File "/oname=maxpack-thumbs.dll" "${THUMBS}"
  File "/oname=LICENSE.txt" "${LICENSE}"
  File "/oname=THIRD_PARTY_NOTICES.txt" "${NOTICES}"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateShortcut "$SMPROGRAMS\MaxPack.lnk" "$INSTDIR\MaxPack.exe"
  CreateShortcut "$DESKTOP\MaxPack.lnk" "$INSTDIR\MaxPack.exe"

  ; MaxPack in "Installed apps", for this user
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "MaxPack"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\MaxPack.exe,0"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "DeLFuS77"
  WriteRegStr HKCU "${UNINSTKEY}" "URLInfoAbout" "https://github.com/DeLFuS77/maxpack"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1

  ; the places in the Explorer ticked, added by MaxPack as its Settings do, the menu in the language of the
  ; installer; none ticked adds nothing and takes nothing away (an update keeps what Settings added)
  StrCpy $0 ""
  ${If} $MenuState = ${BST_CHECKED}
    StrCpy $0 "$0 menu"
  ${EndIf}
  ${If} $SendToState = ${BST_CHECKED}
    StrCpy $0 "$0 sendto"
  ${EndIf}
  ${If} $FilesState = ${BST_CHECKED}
    StrCpy $0 "$0 files"
  ${EndIf}
  ${If} $0 != ""
    Call LangCode
    nsExec::ExecToLog '"$INSTDIR\MaxPack.exe" --installer add$0 --language $1'
    Pop $2
    ${If} $2 != 0
      DetailPrint "$(ExplorerFailed)"
    ${EndIf}
  ${EndIf}
SectionEnd

Section "Uninstall"
  ; what MaxPack added to the Explorer, whatever copy of it it is for, by MaxPack while it is still there; by
  ; the uninstaller when it cannot run
  StrCpy $0 "missing"
  ${If} ${FileExists} "$INSTDIR\MaxPack.exe"
    nsExec::ExecToLog '"$INSTDIR\MaxPack.exe" --installer remove'
    Pop $0
  ${EndIf}
  ${If} $0 != 0
    Call un.RemoveExplorer
  ${EndIf}
  Delete "$INSTDIR\MaxPack.exe"
  Delete "$INSTDIR\maxpack-core.exe"
  Delete "$INSTDIR\maxpack-par2.exe"
  ; the Explorer may still have the thumbnail provider loaded (its keys are gone: it loads it no more). A loaded
  ; DLL can be moved but not deleted: it goes to the temporary folder, so that this folder can go, and is deleted
  ; there now if it can, else at a restart when Windows takes that request. On another drive than the temporary
  ; folder it can only be set aside here. Never its own path at a restart: a later install would have its DLL there
  Delete "$INSTDIR\maxpack-thumbs.dll"
  ${If} ${FileExists} "$INSTDIR\maxpack-thumbs.dll"
    System::Call "kernel32::GetTickCount() i .r0"
    Rename "$INSTDIR\maxpack-thumbs.dll" "$TEMP\maxpack-thumbs-$0.old"
    Delete /REBOOTOK "$TEMP\maxpack-thumbs-$0.old"
    ${If} ${FileExists} "$INSTDIR\maxpack-thumbs.dll"
      Rename "$INSTDIR\maxpack-thumbs.dll" "$INSTDIR\maxpack-thumbs-$0.old"
      Delete /REBOOTOK "$INSTDIR\maxpack-thumbs-$0.old"
    ${EndIf}
  ${EndIf}
  Delete "$INSTDIR\maxpack-thumbs-*.old"
  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\THIRD_PARTY_NOTICES.txt"
  Delete "$INSTDIR\uninstall.exe"
  ; whatever the user put next to them stays, and the folder with it
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\MaxPack.lnk"
  Delete "$DESKTOP\MaxPack.lnk"
  DeleteRegKey HKCU "${UNINSTKEY}"
  ; silently the settings stay; with the window it asks
  MessageBox MB_YESNO|MB_ICONQUESTION "$(KeepSettings)" /SD IDNO IDNO keep
  RMDir /r "$APPDATA\MaxPack"
  RMDir /r "$APPDATA\maxpack.exe"
  keep:
SectionEnd
