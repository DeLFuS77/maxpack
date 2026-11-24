; SPDX-License-Identifier: GPL-3.0-or-later
;
; MaxPack's installer for Windows. It is built with makensis and these definitions:
;   VERSION  (for example dev-1a2b3c4 or 1.2.3)    NUMBERS  (4 numbers: 1.2.3.0)
;   PROGRAM  (MaxPack.exe)   CORE (maxpack-core.exe)   LICENSE (LICENSE)   NOTICES (THIRD_PARTY_NOTICES)
;   ICON (appicon.ico)
;   OUTFILE  (the installer that comes out)
;
; Installs for the user, in %LOCALAPPDATA%\Programs\MaxPack, without administrator rights. It does not carry the
; "settings" folder (that is what makes the zip portable): MaxPack keeps its own in %APPDATA%.
; An already installed MaxPack is updated in its folder.

Unicode true
ManifestDPIAware true
RequestExecutionLevel user
SetCompressor /SOLID lzma

!include MUI2.nsh
!include LogicLib.nsh

!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\MaxPack"

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

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${LICENSE}"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\MaxPack.exe"
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; The languages of the window, the same eleven. The installer takes the one of Windows when it has it; English is
; first, so it is the one used when Windows' is none of them. KeepSettings is the only text of this script that is
; not one of the standard pages, so it is the only one with its own translation.
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

Section "MaxPack"
  SetOutPath "$INSTDIR"
  ; the window looks for the engine next to it
  File "/oname=MaxPack.exe" "${PROGRAM}"
  File "/oname=maxpack-core.exe" "${CORE}"
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
SectionEnd

Section "Uninstall"
  Delete "$INSTDIR\MaxPack.exe"
  Delete "$INSTDIR\maxpack-core.exe"
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
