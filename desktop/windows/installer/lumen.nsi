; Lumen — eigener Streaming-Server fürs Heimnetz.
; Copyright (C) 2026 Die Lumen-Autoren
;
; Veröffentlicht unter der GNU Affero General Public License v3.0.
; Der vollständige Text steht in der Datei LICENSE.
; SPDX-License-Identifier: AGPL-3.0-only
;
; Installer für Lumen für Windows (NSIS 3).
;
;   makensis /DVERSION=0.4.0 /DEXE=..\Lumen.exe /DAUSGABE=..\Lumen-Setup-0.4.0-windows-x64.exe
;            [/DWEBVIEW2_SETUP=MicrosoftEdgeWebview2Setup.exe] [/DLIBMPV=..\libmpv-2.dll]
;            [/DDOKU=..\..\..] lumen.nsi
;
; LIBMPV: libmpv-2.dll für den eigenen Player (mpv, GPL-3.0-or-later; siehe
; THIRD_PARTY.md). Ohne sie spielt Lumen im Player der Web-App.
;
; Installiert wird für den angemeldeten Benutzer nach
; %LOCALAPPDATA%\Programs\Lumen — ohne Administratorrechte, ohne
; UAC-Abfrage, wie bei vielen Programmen, die sich selbst aktuell halten.
; Fehlt die WebView2-Laufzeit (selten, nur auf altem Windows 10), startet
; der Installer den kleinen Bootstrapper von Microsoft, der sie holt.

Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef EXE
  !define EXE "..\Lumen.exe"
!endif
!ifndef AUSGABE
  !define AUSGABE "Lumen-Setup-${VERSION}-windows-x64.exe"
!endif
!ifndef DOKU
  !define DOKU "..\..\.."
!endif

!define NAME "Lumen"
!define HERAUSGEBER "Die Lumen-Autoren"
!define QUELLE "https://github.com/ada462790-beep/lumen-media"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Lumen"
!define FENSTERKLASSE "LumenDesktopFenster"
; WebView2-Laufzeit (Evergreen), so steht es in Microsofts Anleitung.
!define WEBVIEW2_ID "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

Name "${NAME}"
OutFile "${AUSGABE}"
InstallDir "$LOCALAPPDATA\Programs\Lumen"
InstallDirRegKey HKCU "Software\Lumen" "Pfad"
RequestExecutionLevel user
BrandingText "Lumen ${VERSION}"

; Versionsangaben am Installer selbst (Eigenschaften › Details).
VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=0 "ProductName" "Lumen"
VIAddVersionKey /LANG=0 "FileDescription" "Lumen Setup"
VIAddVersionKey /LANG=0 "CompanyName" "${HERAUSGEBER}"
VIAddVersionKey /LANG=0 "LegalCopyright" "© 2026 ${HERAUSGEBER} · AGPL-3.0-only"
VIAddVersionKey /LANG=0 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=0 "ProductVersion" "${VERSION}"

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "x64.nsh"
!include "WinMessages.nsh"

!define MUI_ICON "lumen.ico"
!define MUI_UNICON "lumen.ico"
!define MUI_ABORTWARNING

!define MUI_WELCOMEPAGE_TITLE "$(TitelWillkommen)"
!define MUI_WELCOMEPAGE_TEXT "$(TextWillkommen)"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_TEXT "$(TextFertig)"
!define MUI_FINISHPAGE_RUN "$INSTDIR\Lumen.exe"
!define MUI_FINISHPAGE_RUN_TEXT "$(TextStarten)"
; Der zweite Haken der Abschlussseite legt das Symbol auf den Desktop.
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_SHOWREADME_TEXT "$(TextDesktop)"
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION DesktopSymbol
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "English"

; Eigene Texte. Die deutschen von NSIS schreiben „schließen" — Lumen
; schreibt Schweizer Hochdeutsch und meidet ß-Wörter, damit es überall passt.
LangString TitelWillkommen ${LANG_GERMAN} "Lumen installieren"
LangString TitelWillkommen ${LANG_ENGLISH} "Install Lumen"
LangString TextWillkommen ${LANG_GERMAN} "Lumen bringt deine Filme und Serien von deinem Lumen-Server in ein eigenes Fenster – mit eigenem Player (mpv), der jede Datei direkt abspielt, Vollbild, Medientasten und mehreren Servern.$\r$\n$\r$\nLumen wird nur für dich installiert und braucht keine Administratorrechte.$\r$\n$\r$\n$_CLICK"
LangString TextWillkommen ${LANG_ENGLISH} "Lumen brings the movies and shows on your Lumen server into a window of its own – with its own player (mpv) that plays every file directly, full screen, media keys and multiple servers.$\r$\n$\r$\nLumen is installed just for you and doesn't need administrator rights.$\r$\n$\r$\n$_CLICK"
LangString TextFertig ${LANG_GERMAN} "Lumen ist installiert. Du findest es im Startmenü.$\r$\n$\r$\nBeim ersten Start gibst du die Adresse deines Servers ein, etwa 192.168.10.10:8097."
LangString TextFertig ${LANG_ENGLISH} "Lumen is installed. You'll find it in the Start menu.$\r$\n$\r$\nWhen you first open it, enter your server's address, for example 192.168.10.10:8097."
LangString TextStarten ${LANG_GERMAN} "Lumen jetzt öffnen"
LangString TextStarten ${LANG_ENGLISH} "Open Lumen now"
LangString TextDesktop ${LANG_GERMAN} "Symbol auf dem Desktop"
LangString TextDesktop ${LANG_ENGLISH} "Desktop shortcut"
LangString TextWebView2 ${LANG_GERMAN} "Lumen braucht die Microsoft-Edge-WebView2-Laufzeit. Sie wird jetzt von Microsoft geladen …"
LangString TextWebView2 ${LANG_ENGLISH} "Lumen needs the Microsoft Edge WebView2 Runtime. Downloading it from Microsoft …"
LangString TextWebView2Fehlt ${LANG_GERMAN} "Die WebView2-Laufzeit liess sich nicht installieren (keine Verbindung?). Lumen fragt beim Start noch einmal danach."
LangString TextWebView2Fehlt ${LANG_ENGLISH} "The WebView2 Runtime couldn't be installed (no connection?). Lumen will ask again when it starts."
LangString TextDatenWeg ${LANG_GERMAN} "Auch die Serverliste und die Anmeldungen von Lumen auf diesem Rechner löschen?"
LangString TextDatenWeg ${LANG_ENGLISH} "Also delete Lumen's server list and sign-ins on this computer?"

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "Lumen für Windows braucht ein 64-Bit-Windows (10 oder 11).$\r$\nLumen for Windows needs 64-bit Windows (10 or 11)."
    Abort
  ${EndIf}
FunctionEnd

; Ein laufendes Lumen höflich bitten, sich zu schliessen (es merkt sich
; dabei die Fensterlage), sonst fragen.
!macro LumenSchliessen
  FindWindow $0 "${FENSTERKLASSE}"
  ${If} $0 <> 0
    SendMessage $0 ${WM_CLOSE} 0 0 /TIMEOUT=3000
    StrCpy $1 0
    ${Do}
      Sleep 250
      FindWindow $0 "${FENSTERKLASSE}"
      IntOp $1 $1 + 1
      ${If} $0 = 0
      ${OrIf} $1 > 20
        ${Break}
      ${EndIf}
    ${Loop}
  ${EndIf}
!macroend

Section "Lumen" SecLumen
  SectionIn RO
  !insertmacro LumenSchliessen

  SetOutPath "$INSTDIR"
  ; Ist die Datei noch gesperrt, bietet NSIS selbst „Wiederholen" an.
  File "/oname=Lumen.exe" "${EXE}"
!ifdef LIBMPV
  File "/oname=libmpv-2.dll" "${LIBMPV}"
!endif
  ; Die AGPL verlangt, dass der Lizenztext jede Kopie begleitet.
  File "/oname=LICENSE.txt" "${DOKU}\LICENSE"
  File "/oname=LICENSE-EXCEPTIONS.txt" "${DOKU}\LICENSE-EXCEPTIONS"
  File "/oname=THIRD_PARTY.md" "${DOKU}\THIRD_PARTY.md"
  File "/oname=DRITTSOFTWARE.md" "${DOKU}\DRITTSOFTWARE.md"

  WriteUninstaller "$INSTDIR\Deinstallieren.exe"
  CreateShortcut "$SMPROGRAMS\Lumen.lnk" "$INSTDIR\Lumen.exe" "" "$INSTDIR\Lumen.exe" 0

  WriteRegStr HKCU "Software\Lumen" "Pfad" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "Lumen"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\Lumen.exe,0"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${HERAUSGEBER}"
  WriteRegStr HKCU "${UNINST_KEY}" "URLInfoAbout" "${QUELLE}"
  WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\Deinstallieren.exe"'
  WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\Deinstallieren.exe" /S'
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
SectionEnd

Section "WebView2" SecWebView2
  ; Schon da? Maschinenweit (32-Bit-Ansicht der Registry) oder nur für
  ; diesen Benutzer. „0.0.0.0" heisst: deinstalliert.
  SetRegView 32
  ReadRegStr $0 HKLM "SOFTWARE\Microsoft\EdgeUpdate\Clients\${WEBVIEW2_ID}" "pv"
  SetRegView lastused
  ${If} $0 == ""
  ${OrIf} $0 == "0.0.0.0"
    ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\${WEBVIEW2_ID}" "pv"
  ${EndIf}
  ${If} $0 != ""
  ${AndIf} $0 != "0.0.0.0"
    DetailPrint "WebView2 $0"
    Return
  ${EndIf}
!ifdef WEBVIEW2_SETUP
  DetailPrint "$(TextWebView2)"
  InitPluginsDir
  File "/oname=$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" "${WEBVIEW2_SETUP}"
  ExecWait '"$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install' $1
  ${If} $1 <> 0
    DetailPrint "WebView2-Bootstrapper: $1"
    MessageBox MB_ICONEXCLAMATION "$(TextWebView2Fehlt)" /SD IDOK
  ${EndIf}
!else
  DetailPrint "$(TextWebView2Fehlt)"
!endif
SectionEnd

Function DesktopSymbol
  CreateShortcut "$DESKTOP\Lumen.lnk" "$INSTDIR\Lumen.exe" "" "$INSTDIR\Lumen.exe" 0
FunctionEnd

Section "Uninstall"
  !insertmacro LumenSchliessen
  Delete "$INSTDIR\Lumen.exe"
  Delete "$INSTDIR\libmpv-2.dll"
  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\LICENSE-EXCEPTIONS.txt"
  Delete "$INSTDIR\THIRD_PARTY.md"
  Delete "$INSTDIR\DRITTSOFTWARE.md"
  Delete "$INSTDIR\Deinstallieren.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\Lumen.lnk"
  Delete "$DESKTOP\Lumen.lnk"
  DeleteRegKey HKCU "${UNINST_KEY}"
  DeleteRegKey HKCU "Software\Lumen"
  ; Einstellungen und Anmeldungen nur auf Nachfrage — wer neu installiert,
  ; will seine Server meist behalten. Ohne Rückfrage (/S) bleiben sie.
  MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "$(TextDatenWeg)" /SD IDNO IDNO behalten
    RMDir /r "$APPDATA\Lumen"
    RMDir /r "$LOCALAPPDATA\Lumen"
  behalten:
SectionEnd
