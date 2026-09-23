; Copyright 2026 Regnant
; SPDX-License-Identifier: Apache-2.0
;
; KNOTT for Windows — installer.
;
; A configurable installer rather than a zip:
;   * per-user (default, no administrator rights) or for all users
;   * choose the folder
;   * optional components: the `knott` command-line tool (and adding it to
;     PATH), Start menu and desktop shortcuts, start at sign-in
;   * installs the WebView2 runtime when the machine lacks it
;   * uninstaller that keeps, or on request removes, workflow data
;   * silent installs for fleet deployment:
;       KNOTT-Setup.exe /S [/CURRENTUSER | /ALLUSERS] [/D=C:\Path\To\KNOTT]
;
; Build (from the repository root):
;   makensis -DVERSION=1.2.0 -DSOURCE=dist\win-amd64 -DOUTFILE=dist\KNOTT-1.2.0-windows-x64-setup.exe build\windows\installer.nsi
;
; SOURCE must hold desktop\KNOTT.exe (the desktop app), cli\knott.exe (the CLI)
; and the licence files. build\windows\MicrosoftEdgeWebview2Setup.exe is embedded when
; present.

Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef SOURCE
  !error "Pass -DSOURCE=<folder with desktop\KNOTT.exe and cli\knott.exe>"
!endif
!ifndef OUTFILE
  !define OUTFILE "KNOTT-${VERSION}-setup.exe"
!endif
!ifndef ARCH
  !define ARCH "x64"
!endif

!define APPNAME "KNOTT"
!define PUBLISHER "Regnant"
!define APPID "io.regnant.knott"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}"
!define WEBVIEW2_GUID "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

; ── Per-user or per-machine ──────────────────────────────────────────────────
!define MULTIUSER_EXECUTIONLEVEL Highest
!define MULTIUSER_MUI
!define MULTIUSER_INSTALLMODE_COMMANDLINE
!define MULTIUSER_INSTALLMODE_DEFAULT_CURRENTUSER
!define MULTIUSER_INSTALLMODE_INSTDIR "${APPNAME}"
!define MULTIUSER_INSTALLMODE_INSTALL_REGISTRY_KEY "Software\${PUBLISHER}\${APPNAME}"
!define MULTIUSER_INSTALLMODE_INSTALL_REGISTRY_VALUENAME "InstallDir"
!define MULTIUSER_INSTALLMODE_UNINSTALL_REGISTRY_KEY "Software\${PUBLISHER}\${APPNAME}"
!define MULTIUSER_INSTALLMODE_UNINSTALL_REGISTRY_VALUENAME "InstallDir"
!if "${ARCH}" == "x64"
  !define MULTIUSER_USE_PROGRAMFILES64
!endif
!if "${ARCH}" == "arm64"
  !define MULTIUSER_USE_PROGRAMFILES64
!endif
!include MultiUser.nsh
!include MUI2.nsh
!include LogicLib.nsh
!include FileFunc.nsh
!include x64.nsh

Name "${APPNAME}"
OutFile "${OUTFILE}"
BrandingText "${APPNAME} ${VERSION}"
VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "${APPNAME}"
VIAddVersionKey "CompanyName" "${PUBLISHER}"
VIAddVersionKey "FileDescription" "${APPNAME} installer"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "Copyright 2026 ${PUBLISHER}"

!define MUI_ICON "..\..\brand\icons\knott.ico"
!define MUI_UNICON "..\..\brand\icons\knott.ico"
!define MUI_ABORTWARNING
!define MUI_COMPONENTSPAGE_SMALLDESC
!define MUI_WELCOMEPAGE_TITLE "Install ${APPNAME} ${VERSION}"
!define MUI_WELCOMEPAGE_TEXT "KNOTT builds and runs automated, AI-assisted workflows entirely on this computer.$\r$\n$\r$\nYou will be able to choose where it is installed, whether it is available to every user, and which shortcuts to create.$\r$\n$\r$\nClick Next to continue."
!define MUI_FINISHPAGE_RUN "$INSTDIR\KNOTT.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open ${APPNAME} now"
!define MUI_FINISHPAGE_LINK "Documentation"
!define MUI_FINISHPAGE_LINK_LOCATION "https://github.com/regnant-io/knott#readme"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "..\..\LICENSE"
!insertmacro MULTIUSER_PAGE_INSTALLMODE
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

; ── Sections ─────────────────────────────────────────────────────────────────

Section "!${APPNAME} desktop app" SecCore
  SectionIn RO
  ; A running copy holds its files open; ask it to close first.
  nsExec::Exec 'taskkill /IM KNOTT.exe'
  Sleep 800

  SetOutPath "$INSTDIR"
  File "/oname=KNOTT.exe" "${SOURCE}\desktop\KNOTT.exe"
  File "${SOURCE}\LICENSE"
  File "${SOURCE}\NOTICE"
  File "..\..\brand\icons\knott.ico"
  File "/oname=knott-notification.png" "..\..\brand\icons\knott-64.png"

  Call EnsureWebView2

  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr SHCTX "${UNINSTKEY}" "DisplayName" "${APPNAME}"
  WriteRegStr SHCTX "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr SHCTX "${UNINSTKEY}" "Publisher" "${PUBLISHER}"
  WriteRegStr SHCTX "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\KNOTT.exe"
  WriteRegStr SHCTX "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr SHCTX "${UNINSTKEY}" "URLInfoAbout" "https://github.com/regnant-io/knott"
  WriteRegStr SHCTX "${UNINSTKEY}" "UninstallString" '"$INSTDIR\Uninstall.exe" /$MultiUser.InstallMode'
  WriteRegStr SHCTX "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /$MultiUser.InstallMode /S'
  WriteRegDWORD SHCTX "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD SHCTX "${UNINSTKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD SHCTX "${UNINSTKEY}" "EstimatedSize" "$0"
  WriteRegStr SHCTX "Software\${PUBLISHER}\${APPNAME}" "InstallDir" "$INSTDIR"
SectionEnd

Section "Command-line tool (knott)" SecCLI
  SetOutPath "$INSTDIR\bin"
  File "/oname=knott.exe" "${SOURCE}\cli\knott.exe"
SectionEnd

Section "Add knott to PATH" SecPath
  ; PowerShell edits PATH: NSIS strings are limited to 1024 characters and
  ; would silently truncate a long PATH.
  ${If} $MultiUser.InstallMode == "AllUsers"
    StrCpy $0 "Machine"
  ${Else}
    StrCpy $0 "User"
  ${EndIf}
  nsExec::ExecToLog `powershell -NoProfile -ExecutionPolicy Bypass -Command "$$d='$INSTDIR\bin'; $$p=[Environment]::GetEnvironmentVariable('Path','$0'); if (-not (($$p -split ';') -contains $$d)) { [Environment]::SetEnvironmentVariable('Path', ($$p.TrimEnd(';') + ';' + $$d), '$0') }"`
  WriteRegDWORD SHCTX "Software\${PUBLISHER}\${APPNAME}" "AddedToPath" 1
SectionEnd

Section "Start menu shortcut" SecStartMenu
  CreateShortCut "$SMPROGRAMS\${APPNAME}.lnk" "$INSTDIR\KNOTT.exe" "" "$INSTDIR\KNOTT.exe" 0
SectionEnd

Section "Desktop shortcut" SecDesktop
  CreateShortCut "$DESKTOP\${APPNAME}.lnk" "$INSTDIR\KNOTT.exe" "" "$INSTDIR\KNOTT.exe" 0
SectionEnd

Section /o "Start ${APPNAME} when I sign in" SecAutostart
  ; Schedules and polling triggers only fire while KNOTT is running.
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${APPNAME}" '"$INSTDIR\KNOTT.exe"'
SectionEnd

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecCore} "The ${APPNAME} app. Required."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecCLI} "The knott command for terminals and scripts: run KNOTT as a server, print its data folder, check its version."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecPath} "Lets you type knott in any new terminal."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecStartMenu} "Add ${APPNAME} to the Start menu."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecDesktop} "Add a ${APPNAME} shortcut to the desktop."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecAutostart} "Open ${APPNAME} automatically so scheduled workflows keep running."
!insertmacro MUI_FUNCTION_DESCRIPTION_END

; ── WebView2 ─────────────────────────────────────────────────────────────────
; Windows 11 ships the runtime; some Windows 10 machines do not.
Function EnsureWebView2
  ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\${WEBVIEW2_GUID}" "pv"
  ${If} $0 == ""
  ${OrIf} $0 == "0.0.0.0"
    ReadRegStr $0 HKLM "SOFTWARE\Microsoft\EdgeUpdate\Clients\${WEBVIEW2_GUID}" "pv"
  ${EndIf}
  ${If} $0 == ""
  ${OrIf} $0 == "0.0.0.0"
    ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\${WEBVIEW2_GUID}" "pv"
  ${EndIf}
  ${If} $0 != ""
  ${AndIf} $0 != "0.0.0.0"
    DetailPrint "WebView2 runtime $0 is installed."
    Return
  ${EndIf}
  !if /FileExists "MicrosoftEdgeWebview2Setup.exe"
    DetailPrint "Installing the Microsoft Edge WebView2 runtime…"
    SetOutPath "$PLUGINSDIR"
    File "MicrosoftEdgeWebview2Setup.exe"
    ExecWait '"$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install' $1
    DetailPrint "WebView2 installer finished with code $1."
  !else
    MessageBox MB_ICONINFORMATION|MB_OK "${APPNAME} needs the Microsoft Edge WebView2 runtime, which this computer does not have.$\r$\n$\r$\nInstall it from https://developer.microsoft.com/microsoft-edge/webview2/ and then open ${APPNAME}." /SD IDOK
  !endif
FunctionEnd

; ── Installer lifecycle ──────────────────────────────────────────────────────
Function .onInit
  !insertmacro MULTIUSER_INIT
  ${If} ${RunningX64}
    SetRegView 64
  ${EndIf}
FunctionEnd

Function un.onInit
  !insertmacro MULTIUSER_UNINIT
  ${If} ${RunningX64}
    SetRegView 64
  ${EndIf}
FunctionEnd

; ── Uninstaller ──────────────────────────────────────────────────────────────
Section "Uninstall"
  nsExec::Exec 'taskkill /IM KNOTT.exe'
  Sleep 800

  ReadRegDWORD $0 SHCTX "Software\${PUBLISHER}\${APPNAME}" "AddedToPath"
  ${If} $0 == 1
    ${If} $MultiUser.InstallMode == "AllUsers"
      StrCpy $1 "Machine"
    ${Else}
      StrCpy $1 "User"
    ${EndIf}
    nsExec::ExecToLog `powershell -NoProfile -ExecutionPolicy Bypass -Command "$$d='$INSTDIR\bin'; $$p=[Environment]::GetEnvironmentVariable('Path','$1'); [Environment]::SetEnvironmentVariable('Path', (($$p -split ';' | Where-Object { $$_ -and $$_ -ne $$d }) -join ';'), '$1')"`
  ${EndIf}

  Delete "$SMPROGRAMS\${APPNAME}.lnk"
  Delete "$DESKTOP\${APPNAME}.lnk"
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${APPNAME}"

  Delete "$INSTDIR\KNOTT.exe"
  Delete "$INSTDIR\bin\knott.exe"
  RMDir "$INSTDIR\bin"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\NOTICE"
  Delete "$INSTDIR\knott.ico"
  Delete "$INSTDIR\knott-notification.png"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  DeleteRegKey SHCTX "${UNINSTKEY}"
  DeleteRegKey SHCTX "Software\${PUBLISHER}\${APPNAME}"

  ; Workflows, run history and credentials belong to the user, so they stay
  ; unless the user says otherwise (a silent uninstall always keeps them).
  MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "Also delete your KNOTT workflows, run history and stored credentials?$\r$\n$\r$\n($LOCALAPPDATA\KNOTT)" /SD IDNO IDNO keep
    RMDir /r "$LOCALAPPDATA\KNOTT"
  keep:
SectionEnd
