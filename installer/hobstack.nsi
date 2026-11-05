; Installerul HobStack pentru Windows (NSIS 3, Modern UI 2; caile relative sunt fata de folderul installer/).
; Il construieste scripts/build-release.sh (SRC absolut):
;   makensis -DVERSION=1.0.5 -DSRC=<folderul cu hobstack.exe, README.md, LICENSE> -DOUTFILE=<setup.exe> installer/hobstack.nsi
; Instaleaza pentru utilizatorul curent, fara admin (implicit %LOCALAPPDATA%\Programs\HobStack; folderul se alege).
; Langa exe pune hobstack.portable: HobStack tine atunci datele (baza, log-uri, fisiere, backup-uri) in <folder>\data.

Unicode true
!include "MUI2.nsh"
!include "LogicLib.nsh"

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef SRC
  !error "SRC (the folder with hobstack.exe) is required"
!endif
!ifndef OUTFILE
  !define OUTFILE "HobStack-Setup-${VERSION}.exe"
!endif

!define UNINST "Software\Microsoft\Windows\CurrentVersion\Uninstall\HobStack"
!define RUNKEY "Software\Microsoft\Windows\CurrentVersion\Run"

Name "HobStack"
Caption "HobStack ${VERSION} Setup"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\HobStack"
InstallDirRegKey HKCU "Software\HobStack" "InstallDir"
RequestExecutionLevel user
SetCompressor /SOLID lzma
BrandingText "HobStack ${VERSION}"

VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "HobStack"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "FileDescription" "HobStack installer"
VIAddVersionKey "LegalCopyright" "MIT License"

!define MUI_ICON "../assets/hobstack.ico"
!define MUI_UNICON "../assets/hobstack.ico"
!define MUI_ABORTWARNING
!define MUI_COMPONENTSPAGE_NODESC
!define MUI_FINISHPAGE_RUN "$INSTDIR\hobstack.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Start HobStack"

!define MUI_WELCOMEPAGE_TEXT "This installs HobStack ${VERSION}, your local Claude Code agent teams.$\r$\n$\r$\nHobStack keeps its data (projects, tasks, logs) in a data folder next to the program. If you used HobStack before, your data moves there the first time it starts.$\r$\n$\r$\nClick Next to continue."
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

; StopHobStack opreste HobStack-ul care ruleaza (oricare, dupa port), cu exe-ul dat: "hobstack quit" intoarce
; 0 = oprit sau nu rula, 2 = lucreaza agenti. Cu agenti intreaba inainte; Nu = installerul se opreste.
!macro StopHobStack EXE
  nsExec::Exec '"${EXE}" quit'
  Pop $0
  ${If} $0 == 2
    MessageBox MB_YESNO|MB_ICONQUESTION "HobStack agents are working right now. Stop them and continue?$\r$\n$\r$\nTheir tasks go back to Queued and continue when HobStack starts again." IDYES +2
      Abort
    nsExec::Exec '"${EXE}" quit --force'
    Pop $0
  ${EndIf}
!macroend

Section "HobStack" SecMain
  SectionIn RO
  ; exe-ul nou opreste HobStack-ul care ruleaza (si o versiune veche, pornita din alt folder)
  InitPluginsDir
  File "/oname=$PLUGINSDIR\hobstack.exe" "${SRC}/hobstack.exe"
  !insertmacro StopHobStack "$PLUGINSDIR\hobstack.exe"

  SetOutPath "$INSTDIR"
  File "${SRC}/hobstack.exe"
  File "${SRC}/README.md"
  File "${SRC}/LICENSE"
  Delete "$INSTDIR\hobstack.exe.new"
  FileOpen $1 "$INSTDIR\hobstack.portable" w
  FileWrite $1 "HobStack keeps its data in the data folder next to this file.$\r$\n"
  FileClose $1

  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "Software\HobStack" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "${UNINST}" "DisplayName" "HobStack"
  WriteRegStr HKCU "${UNINST}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST}" "Publisher" "HobStack"
  WriteRegStr HKCU "${UNINST}" "DisplayIcon" "$INSTDIR\hobstack.exe"
  WriteRegStr HKCU "${UNINST}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "${UNINST}" "URLInfoAbout" "https://github.com/vladutt/hobstack"
  WriteRegDWORD HKCU "${UNINST}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST}" "NoRepair" 1
SectionEnd

Section "Desktop shortcut" SecDesktop
  CreateShortcut "$DESKTOP\HobStack.lnk" "$INSTDIR\hobstack.exe" "" "$INSTDIR\hobstack.exe" 0
SectionEnd

Section "Start menu shortcut" SecStartMenu
  CreateShortcut "$SMPROGRAMS\HobStack.lnk" "$INSTDIR\hobstack.exe" "" "$INSTDIR\hobstack.exe" 0
SectionEnd

Section "Uninstall"
  ${If} ${FileExists} "$INSTDIR\hobstack.exe"
    !insertmacro StopHobStack "$INSTDIR\hobstack.exe"
  ${EndIf}

  DeleteRegValue HKCU "${RUNKEY}" "HobStack" ; pornirea la login
  Delete "$DESKTOP\HobStack.lnk"
  Delete "$SMPROGRAMS\HobStack.lnk"
  Delete "$INSTDIR\hobstack.exe"
  Delete "$INSTDIR\hobstack.exe.prev"
  Delete "$INSTDIR\hobstack.exe.new"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\hobstack.portable"
  Delete "$INSTDIR\Uninstall.exe"

  IfFileExists "$INSTDIR\data\*.*" 0 done
    MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "Also delete your HobStack data (projects, tasks, history and logs) in $INSTDIR\data?$\r$\n$\r$\nChoose No to keep it; installing HobStack again in this folder uses it." /SD IDNO IDNO done
    RMDir /r "$INSTDIR\data"
  done:
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "${UNINST}"
  DeleteRegKey HKCU "Software\HobStack"
SectionEnd
