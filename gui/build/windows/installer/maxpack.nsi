; SPDX-License-Identifier: GPL-3.0-or-later
;
; El instalador de MaxPack para Windows. Se compila con makensis y estas definiciones:
;   VERSION  (por ejemplo dev-1a2b3c4 o 1.2.3)    NUMBERS  (4 números: 1.2.3.0)
;   PROGRAM  (MaxPack.exe)   CORE (maxpack-core.exe)   LICENSE (LICENSE)   NOTICES (THIRD_PARTY_NOTICES)
;   ICON (appicon.ico)
;   OUTFILE  (el instalador que sale)
;
; Instala para el usuario, en %LOCALAPPDATA%\Programs\MaxPack, sin administrador. No lleva la
; carpeta "settings" (eso es lo que hace portable al zip): MaxPack guarda lo suyo en %APPDATA%.
; Un MaxPack ya instalado se actualiza en su carpeta.

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

; el español primero: es el idioma por defecto si el de Windows no está entre los dos
!insertmacro MUI_LANGUAGE "Spanish"
!insertmacro MUI_LANGUAGE "English"

LangString KeepSettings ${LANG_SPANISH} "¿Borrar también los ajustes de MaxPack y lo que recuerda de la ventana?"
LangString KeepSettings ${LANG_ENGLISH} "Also delete the settings of MaxPack and what its window remembers?"

Section "MaxPack"
  SetOutPath "$INSTDIR"
  ; la ventana busca el motor junto a ella
  File "/oname=MaxPack.exe" "${PROGRAM}"
  File "/oname=maxpack-core.exe" "${CORE}"
  File "/oname=LICENSE.txt" "${LICENSE}"
  File "/oname=THIRD_PARTY_NOTICES.txt" "${NOTICES}"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateShortcut "$SMPROGRAMS\MaxPack.lnk" "$INSTDIR\MaxPack.exe"
  CreateShortcut "$DESKTOP\MaxPack.lnk" "$INSTDIR\MaxPack.exe"

  ; MaxPack en «Aplicaciones instaladas», para este usuario
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
  ; lo que haya puesto el usuario junto a ellos se queda, y la carpeta con ello
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\MaxPack.lnk"
  Delete "$DESKTOP\MaxPack.lnk"
  DeleteRegKey HKCU "${UNINSTKEY}"
  ; en silencio los ajustes se quedan; con la ventana se pregunta
  MessageBox MB_YESNO|MB_ICONQUESTION "$(KeepSettings)" /SD IDNO IDNO keep
  RMDir /r "$APPDATA\MaxPack"
  RMDir /r "$APPDATA\maxpack.exe"
  keep:
SectionEnd
