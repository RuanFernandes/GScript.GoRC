# DO NOT EDIT - Generated automatically by `wails build`

!include "x64.nsh"
!include "WinVer.nsh"
!include "FileFunc.nsh"
!include "StrFunc.nsh"

${StrStr}
${UnStrStr}

!ifndef INFO_PROJECTNAME
    !error "Graal RC: INFO_PROJECTNAME must be supplied from build/windows/info.json."
!endif
!ifndef INFO_COMPANYNAME
    !error "Graal RC: INFO_COMPANYNAME must be supplied from build/windows/info.json."
!endif
!ifndef INFO_PRODUCTNAME
    !error "Graal RC: INFO_PRODUCTNAME must be supplied from build/windows/info.json."
!endif
!ifndef INFO_PRODUCTVERSION
    !error "Graal RC: INFO_PRODUCTVERSION must be supplied from build/windows/info.json."
!endif
!ifndef INFO_COPYRIGHT
    !error "Graal RC: INFO_COPYRIGHT must be supplied from build/windows/info.json."
!endif
!ifndef PRODUCT_EXECUTABLE
    !define PRODUCT_EXECUTABLE "${INFO_PROJECTNAME}.exe"
!endif
!ifndef UNINST_KEY_NAME
    !define UNINST_KEY_NAME "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
!endif
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${UNINST_KEY_NAME}"

!ifndef WAILS_INSTALL_SCOPE
    !define WAILS_INSTALL_SCOPE "machine"
!endif

!ifndef REQUEST_EXECUTION_LEVEL
    !if "${WAILS_INSTALL_SCOPE}" == "user"
        !define REQUEST_EXECUTION_LEVEL "user"
    !else
        !define REQUEST_EXECUTION_LEVEL "admin"
    !endif
!endif

!if "${WAILS_INSTALL_SCOPE}" != "user"
    !if "${WAILS_INSTALL_SCOPE}" != "machine"
        !error "Graal RC: WAILS_INSTALL_SCOPE must be 'user' or 'machine'."
    !endif
!endif

!if "${WAILS_INSTALL_SCOPE}" == "user"
    !if "${REQUEST_EXECUTION_LEVEL}" != "user"
        !error "Graal RC: user installs must use REQUEST_EXECUTION_LEVEL=user."
    !endif
!else
    !if "${REQUEST_EXECUTION_LEVEL}" != "admin"
        !error "Graal RC: machine installs must use REQUEST_EXECUTION_LEVEL=admin."
    !endif
!endif

RequestExecutionLevel "${REQUEST_EXECUTION_LEVEL}"

!ifdef ARG_WAILS_AMD64_BINARY
    !define SUPPORTS_AMD64
!endif

!ifdef ARG_WAILS_ARM64_BINARY
    !define SUPPORTS_ARM64
!endif

!ifdef SUPPORTS_AMD64
    !ifdef SUPPORTS_ARM64
        !define ARCH "amd64_arm64"
    !else
        !define ARCH "amd64"
    !endif
!else
    !ifdef SUPPORTS_ARM64
        !define ARCH "arm64"
    !else
        !error "Wails: Undefined ARCH, please provide at least one of ARG_WAILS_AMD64_BINARY or ARG_WAILS_ARM64_BINARY"
    !endif
!endif

# Graal RC currently ships only the amd64 native library. Do not produce an
# installer that embeds an executable whose process cannot load it.
!if "${ARCH}" != "amd64"
    !error "Graal RC: the Windows installer requires an amd64 executable because rclib/grclib64.dll is the only native library shipped."
!endif

!ifndef ARG_GRCLIB_DLL
    !error "Graal RC: ARG_GRCLIB_DLL is required and must point to rclib/grclib64.dll."
!endif
!ifndef ARG_GRCLIB_FILE
    !error "Graal RC: ARG_GRCLIB_FILE is required and must be grclib64.dll."
!endif
!if "${ARG_GRCLIB_FILE}" != "grclib64.dll"
    !error "Graal RC: the native library destination must be grclib64.dll for an amd64 build."
!endif

!macro wails.checkArchitecture
    !ifndef WAILS_WIN10_REQUIRED
        !define WAILS_WIN10_REQUIRED "This product is only supported on Windows 10 (Server 2016) and later."
    !endif

    !ifndef WAILS_ARCHITECTURE_NOT_SUPPORTED
        !define WAILS_ARCHITECTURE_NOT_SUPPORTED "This product can't be installed on the current Windows architecture. Supports: ${ARCH}"
    !endif

    ${If} ${AtLeastWin10}
        !ifdef SUPPORTS_AMD64
            ${if} ${IsNativeAMD64}
                Goto ok
            ${EndIf}
        !endif

        !ifdef SUPPORTS_ARM64
            ${if} ${IsNativeARM64}
                Goto ok
            ${EndIf}
        !endif

        IfSilent silentArch notSilentArch
        silentArch:
            SetErrorLevel 65
            Abort
        notSilentArch:
            MessageBox MB_OK "${WAILS_ARCHITECTURE_NOT_SUPPORTED}"
            SetErrorLevel 65
            Abort
    ${else}
        IfSilent silentWin notSilentWin
        silentWin:
            SetErrorLevel 64
            Abort
        notSilentWin:
            MessageBox MB_OK "${WAILS_WIN10_REQUIRED}"
            SetErrorLevel 64
            Abort
    ${EndIf}

    ok:
!macroend

!macro wails.setRegistryView
    !if "${ARCH}" == "386"
        SetRegView 32
    !else
        SetRegView 64
    !endif
!macroend

!macro wails.files
    !ifdef SUPPORTS_AMD64
        ${if} ${IsNativeAMD64}
            File "/oname=${PRODUCT_EXECUTABLE}" "${ARG_WAILS_AMD64_BINARY}"
        ${EndIf}
    !endif

    !ifdef SUPPORTS_ARM64
        ${if} ${IsNativeARM64}
            File "/oname=${PRODUCT_EXECUTABLE}" "${ARG_WAILS_ARM64_BINARY}"
        ${EndIf}
    !endif

!macroend

!macro wails.writeUninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"

    !insertmacro wails.setRegistryView
    !if "${WAILS_INSTALL_SCOPE}" == "user"
        WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
        WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
        WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"

        ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
        IntFmt $0 "0x%08X" $0
        WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
    !else
        WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
        WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
        WriteRegStr HKLM "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"

        ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
        IntFmt $0 "0x%08X" $0
        WriteRegDWORD HKLM "${UNINST_KEY}" "EstimatedSize" "$0"
    !endif
!macroend

!macro wails.deleteUninstaller
    Delete "$INSTDIR\uninstall.exe"

    !insertmacro wails.setRegistryView
    !if "${WAILS_INSTALL_SCOPE}" == "user"
        DeleteRegKey HKCU "${UNINST_KEY}"
    !else
        DeleteRegKey HKLM "${UNINST_KEY}"
    !endif
!macroend

!macro wails.setShellContext
    !if "${WAILS_INSTALL_SCOPE}" == "machine"
        SetShellVarContext all
    !else
        SetShellVarContext current
    !endif
!macroend

!macro wails.checkAppClosed SEARCH_FUNCTION
    ; The bundled Unicode nsExec plug-in returns "error" when /OEM or
    ; /TIMEOUT is passed on current Windows builds. tasklist is a short-lived
    ; local query, so use its default execution path and preserve its output.
    nsExec::ExecToStack '"$SYSDIR\tasklist.exe" /FI "IMAGENAME eq ${PRODUCT_EXECUTABLE}" /FO CSV /NH'
    Pop $R0
    Pop $R1

    ${If} $R0 == "error"
        MessageBox MB_ICONSTOP|MB_OK "${INFO_PRODUCTNAME} could not verify whether the application is running. Close ${INFO_PRODUCTNAME} and run this installer again."
        SetErrorLevel 71
        Abort
    ${EndIf}
    ${If} $R0 == "timeout"
        MessageBox MB_ICONSTOP|MB_OK "The check for a running ${INFO_PRODUCTNAME} process timed out. Close ${INFO_PRODUCTNAME} and run this installer again."
        SetErrorLevel 71
        Abort
    ${EndIf}
    ${If} $R0 != "0"
        MessageBox MB_ICONSTOP|MB_OK "${INFO_PRODUCTNAME} could not verify whether the application is running (task check exit code $R0). Close ${INFO_PRODUCTNAME} and run this installer again."
        SetErrorLevel 71
        Abort
    ${EndIf}

    ${${SEARCH_FUNCTION}} $R2 $R1 "${PRODUCT_EXECUTABLE}"
    ${If} $R2 != ""
        MessageBox MB_ICONEXCLAMATION|MB_OK "${INFO_PRODUCTNAME} is still running. Close it completely, then run this installer again. The operation was cancelled to protect the installed files and user data."
        SetErrorLevel 32
        Abort
    ${EndIf}
!macroend

# Install webview2 by launching the bootstrapper
# See https://docs.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution#online-only-deployment
!macro wails.webview2runtime
    !ifndef WAILS_INSTALL_WEBVIEW_DETAILPRINT
        !define WAILS_INSTALL_WEBVIEW_DETAILPRINT "Installing: WebView2 Runtime"
    !endif

    !insertmacro wails.setRegistryView
	# If the machine key exists and is not empty then WebView2 is already installed.
	ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    ${If} $0 != ""
        Goto webview2_ok
    ${EndIf}

    !if "${WAILS_INSTALL_SCOPE}" == "user"
        # A user-scope bootstrapper can use the per-user WebView2 runtime.
        ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
        ${If} $0 != ""
            Goto webview2_ok
        ${EndIf}
    !endif
    
	SetDetailsPrint both
    DetailPrint "${WAILS_INSTALL_WEBVIEW_DETAILPRINT}"
    SetDetailsPrint listonly
    
    InitPluginsDir
    CreateDirectory "$pluginsdir\webview2bootstrapper"
    SetOutPath "$pluginsdir\webview2bootstrapper"
    File "MicrosoftEdgeWebview2Setup.exe"
    ClearErrors
    ExecWait '"$pluginsdir\webview2bootstrapper\MicrosoftEdgeWebview2Setup.exe" /silent /install' $R0
    ${If} ${Errors}
        SetDetailsPrint both
        DetailPrint "WebView2 Runtime bootstrapper could not be started."
        MessageBox MB_ICONSTOP|MB_OK "${INFO_PRODUCTNAME} could not start the WebView2 Runtime installer. Install the Evergreen WebView2 Runtime manually from https://developer.microsoft.com/microsoft-edge/webview2/ and run this installer again."
        SetErrorLevel 70
        Abort
    ${EndIf}
    # 3010 means the runtime installed successfully and Windows requests a reboot.
    ${If} $R0 != 0
    ${AndIf} $R0 != 3010
        SetDetailsPrint both
        DetailPrint "WebView2 Runtime bootstrapper failed with exit code $R0."
        MessageBox MB_ICONSTOP|MB_OK "${INFO_PRODUCTNAME} could not install the WebView2 Runtime (bootstrapper exit code $R0). Install or repair the Evergreen WebView2 Runtime from https://developer.microsoft.com/microsoft-edge/webview2/, then run this installer again."
        SetErrorLevel 70
        Abort
    ${EndIf}
    
    SetDetailsPrint both
    webview2_ok:
!macroend

# Copy of APP_ASSOCIATE and APP_UNASSOCIATE macros from here https://gist.github.com/nikku/281d0ef126dbc215dd58bfd5b3a5cd5b
!macro APP_ASSOCIATE EXT FILECLASS DESCRIPTION ICON COMMANDTEXT COMMAND
  ; Backup the previously associated file class
  ReadRegStr $R0 SHELL_CONTEXT "Software\Classes\.${EXT}" ""
  WriteRegStr SHELL_CONTEXT "Software\Classes\.${EXT}" "${FILECLASS}_backup" "$R0"

  WriteRegStr SHELL_CONTEXT "Software\Classes\.${EXT}" "" "${FILECLASS}"

  WriteRegStr SHELL_CONTEXT "Software\Classes\${FILECLASS}" "" `${DESCRIPTION}`
  WriteRegStr SHELL_CONTEXT "Software\Classes\${FILECLASS}\DefaultIcon" "" `${ICON}`
  WriteRegStr SHELL_CONTEXT "Software\Classes\${FILECLASS}\shell" "" "open"
  WriteRegStr SHELL_CONTEXT "Software\Classes\${FILECLASS}\shell\open" "" `${COMMANDTEXT}`
  WriteRegStr SHELL_CONTEXT "Software\Classes\${FILECLASS}\shell\open\command" "" `${COMMAND}`
!macroend

!macro APP_UNASSOCIATE EXT FILECLASS
  ; Backup the previously associated file class
  ReadRegStr $R0 SHELL_CONTEXT "Software\Classes\.${EXT}" `${FILECLASS}_backup`
  WriteRegStr SHELL_CONTEXT "Software\Classes\.${EXT}" "" "$R0"

  DeleteRegKey SHELL_CONTEXT `Software\Classes\${FILECLASS}`
!macroend

!macro wails.associateFiles
    ; Create file associations
    
!macroend

!macro wails.unassociateFiles
    ; Delete app associations
    
!macroend

!macro CUSTOM_PROTOCOL_ASSOCIATE PROTOCOL DESCRIPTION ICON COMMAND
  DeleteRegKey SHELL_CONTEXT "Software\Classes\${PROTOCOL}"
  WriteRegStr SHELL_CONTEXT "Software\Classes\${PROTOCOL}" "" "${DESCRIPTION}"
  WriteRegStr SHELL_CONTEXT "Software\Classes\${PROTOCOL}" "URL Protocol" ""
  WriteRegStr SHELL_CONTEXT "Software\Classes\${PROTOCOL}\DefaultIcon" "" "${ICON}"
  WriteRegStr SHELL_CONTEXT "Software\Classes\${PROTOCOL}\shell" "" ""
  WriteRegStr SHELL_CONTEXT "Software\Classes\${PROTOCOL}\shell\open" "" ""
  WriteRegStr SHELL_CONTEXT "Software\Classes\${PROTOCOL}\shell\open\command" "" "${COMMAND}"
!macroend

!macro CUSTOM_PROTOCOL_UNASSOCIATE PROTOCOL
  DeleteRegKey SHELL_CONTEXT "Software\Classes\${PROTOCOL}"
!macroend

!macro wails.associateCustomProtocols
    ; Create custom protocols associations
    
!macroend

!macro wails.unassociateCustomProtocols
    ; Delete app custom protocol associations
    
!macroend
