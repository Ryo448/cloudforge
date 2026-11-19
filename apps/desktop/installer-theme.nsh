!ifndef BUILD_UNINSTALLER
  !include "WinMessages.nsh"

  !define CF_INSTALLER_CANVAS "13171E"
  !define CF_INSTALLER_HEADER "101319"
  !define CF_INSTALLER_TEXT "F1F3F7"
  !define CF_INSTALLER_MUTED "A5AFBF"
  !define MUI_BGCOLOR "${CF_INSTALLER_HEADER}"
  !define MUI_TEXTCOLOR "${CF_INSTALLER_TEXT}"
  !define MUI_HEADERIMAGE
  !define MUI_HEADERIMAGE_RIGHT
  !define MUI_HEADERIMAGE_BITMAP "${PROJECT_DIR}\assets\installer\header.bmp"
  !define MUI_CUSTOMFUNCTION_GUIINIT CloudforgeStyleInstaller

  Var cfInstallPage
  Var cfInstallerTitleFont
  Var cfInstallerBodyFont

  !macro customWelcomePage
    !ifndef INSTALL_MODE_PER_ALL_USERS
      !ifmacrondef licensePage
        !define MUI_PAGE_CUSTOMFUNCTION_SHOW CloudforgeStyleMode
      !endif
    !endif
  !macroend

  # Declare the standard directory page here so we can style its controls.
  !ifdef allowToChangeInstallationDirectory
    !undef allowToChangeInstallationDirectory
    !define CF_INSTALLER_DIRECTORY_PAGE
    !include "StrContains.nsh"
  !endif

  !macro customPageAfterChangeDir
    !ifdef CF_INSTALLER_DIRECTORY_PAGE
      !insertmacro skipPageIfUpdated
      !define MUI_PAGE_CUSTOMFUNCTION_SHOW CloudforgeStyleDirectory
      !insertmacro MUI_PAGE_DIRECTORY
      !define MUI_PAGE_CUSTOMFUNCTION_PRE CloudforgePrepareDirectory
    !endif
    !define MUI_PAGE_CUSTOMFUNCTION_SHOW CloudforgeStyleProgress
    !define MUI_INSTFILESPAGE_PROGRESSBAR colored
    !define MUI_INSTFILESPAGE_COLORS "FF7A1A 272D39"
  !macroend

  # Position stock controls in dialog units so the layout follows Windows DPI scaling.
  !macro CloudforgePosition PARENT CONTROL X Y WIDTH HEIGHT
    System::Call "*(i${X}, i${Y}, i${WIDTH}, i${HEIGHT}) p.r1"
    System::Call "user32::MapDialogRect(p ${PARENT}, p r1)"
    System::Call "*$1(i.r2, i.r3, i.r4, i.r5)"
    System::Free $1
    System::Call "user32::SetWindowPos(p ${CONTROL}, p0, i r2, i r3, i r4, i r5, i 0x14)"
  !macroend

  !macro CloudforgeProgressLabel TEXT FONT COLOR BACKGROUND STYLE X Y WIDTH HEIGHT
    System::Call 'user32::CreateWindowExW(i0, w "STATIC", w "${TEXT}", i ${STYLE}, i0, i0, i0, i0, p $cfInstallPage, p0, p0, p0) p.r0'
    !insertmacro CloudforgePosition $cfInstallPage $0 ${X} ${Y} ${WIDTH} ${HEIGHT}
    SetCtlColors $0 "${COLOR}" "${BACKGROUND}"
    SendMessage $0 ${WM_SETFONT} ${FONT} 1
  !macroend

  !macro CloudforgeStyleChoice CONTROL BACKGROUND
    # The Windows checkbox/radio theme overrides the text color.
    System::Call 'uxtheme::SetWindowTheme(p ${CONTROL}, w " ", w " ")'
    SetCtlColors ${CONTROL} "${CF_INSTALLER_TEXT}" "${BACKGROUND}"
  !macroend

  !macro CloudforgeThemeFunctions
    Function CloudforgeStyleInstaller
      # The caption remains a native window control.
      System::Call "*(i 1) p.r0"
      System::Call "dwmapi::DwmSetWindowAttribute(p $HWNDPARENT, i 20, p r0, i 4)"
      System::Free $0
      CreateFont $cfInstallerTitleFont "Segoe UI" "20" "650"
      CreateFont $cfInstallerBodyFont "Segoe UI" "9" "400"
      SetCtlColors $mui.Header.SubText "${CF_INSTALLER_MUTED}" "${CF_INSTALLER_HEADER}"
      SendMessage $mui.Header.SubText ${WM_SETFONT} $cfInstallerBodyFont 1
      SetCtlColors $HWNDPARENT "${CF_INSTALLER_TEXT}" "${CF_INSTALLER_CANVAS}"
      ShowWindow $mui.Branding.Background ${SW_HIDE}
      ShowWindow $mui.Branding.Text ${SW_HIDE}
      System::Call 'uxtheme::SetWindowTheme(p $mui.Button.Cancel, w "DarkMode_Explorer", p0)'
      System::Call 'uxtheme::SetWindowTheme(p $mui.Button.Back, w "DarkMode_Explorer", p0)'
      System::Call 'uxtheme::SetWindowTheme(p $mui.Button.Next, w "DarkMode_Explorer", p0)'
    FunctionEnd

    !ifndef INSTALL_MODE_PER_ALL_USERS
      !ifmacrondef licensePage
        Function CloudforgeStyleMode
          SetCtlColors $MultiUser.InstallModePage "${CF_INSTALLER_TEXT}" "${CF_INSTALLER_CANVAS}"
          SetCtlColors $MultiUser.InstallModePage.Text "${CF_INSTALLER_MUTED}" "${CF_INSTALLER_CANVAS}"
          SetCtlColors $RadioButtonLabel1 "${CF_INSTALLER_MUTED}" "${CF_INSTALLER_CANVAS}"
          !insertmacro CloudforgeStyleChoice $MultiUser.InstallModePage.AllUsers "${CF_INSTALLER_CANVAS}"
          !insertmacro CloudforgeStyleChoice $MultiUser.InstallModePage.CurrentUser "${CF_INSTALLER_CANVAS}"
        FunctionEnd
      !endif
    !endif

    !ifdef CF_INSTALLER_DIRECTORY_PAGE
      Function CloudforgeStyleDirectory
        SetCtlColors $mui.DirectoryPage "${CF_INSTALLER_TEXT}" "${CF_INSTALLER_CANVAS}"
        SetCtlColors $mui.DirectoryPage.Text "${CF_INSTALLER_MUTED}" "${CF_INSTALLER_CANVAS}"
        SetCtlColors $mui.DirectoryPage.SpaceRequired "${CF_INSTALLER_MUTED}" "${CF_INSTALLER_CANVAS}"
        SetCtlColors $mui.DirectoryPage.SpaceAvailable "${CF_INSTALLER_MUTED}" "${CF_INSTALLER_CANVAS}"
        !insertmacro CloudforgeStyleChoice $mui.DirectoryPage.DirectoryBox "${CF_INSTALLER_CANVAS}"
        SetCtlColors $mui.DirectoryPage.Directory "${CF_INSTALLER_TEXT}" "${CF_INSTALLER_HEADER}"
        System::Call 'uxtheme::SetWindowTheme(p $mui.DirectoryPage.BrowseButton, w "DarkMode_Explorer", p0)'
      FunctionEnd

      Function CloudforgePrepareDirectory
        # Keep electron-builder's application subdirectory normalization.
        ${StrContains} $0 "${APP_FILENAME}" $INSTDIR
        ${If} $0 == ""
          StrCpy $INSTDIR "$INSTDIR\${APP_FILENAME}"
        ${EndIf}
      FunctionEnd
    !endif

    Function CloudforgeStyleProgress
      FindWindow $cfInstallPage "#32770" "" $HWNDPARENT
      ${If} $cfInstallPage == 0
        Return
      ${EndIf}

      SetCtlColors $cfInstallPage "${CF_INSTALLER_TEXT}" "${CF_INSTALLER_CANVAS}"
      ShowWindow $mui.Line.Standard ${SW_HIDE}
      ShowWindow $mui.Line.FullWindow ${SW_HIDE}
      GetDlgItem $0 $HWNDPARENT 1040
      ShowWindow $0 ${SW_HIDE}

      SendMessage $mui.Header.Text ${WM_SETTEXT} 0 "STR:Cloudforge"
      SendMessage $mui.Header.SubText ${WM_SETTEXT} 0 "STR:v${VERSION}"
      ${If} ${isUpdated}
      ${AndIf} ${isForceRun}
        ShowWindow $mui.Button.Back ${SW_HIDE}
        ShowWindow $mui.Button.Next ${SW_HIDE}
      ${EndIf}

      !insertmacro CloudforgeProgressLabel "$(MUI_TEXT_INSTALLING_TITLE)" $cfInstallerTitleFont "${CF_INSTALLER_TEXT}" "${CF_INSTALLER_CANVAS}" 0x50000080 17 19 263 25

      !insertmacro CloudforgeProgressLabel "$(^DirSubText)" $cfInstallerBodyFont "${CF_INSTALLER_MUTED}" "${CF_INSTALLER_CANVAS}" 0x50000080 17 91 263 13
      !insertmacro CloudforgeProgressLabel "$INSTDIR" $cfInstallerBodyFont "${CF_INSTALLER_TEXT}" "${CF_INSTALLER_HEADER}" 0x50008280 17 105 263 18

      GetDlgItem $0 $cfInstallPage 1004
      !insertmacro CloudforgePosition $cfInstallPage $0 17 70 263 9
      # Color the same progress control that NSIS updates during extraction.
      System::Call 'uxtheme::SetWindowTheme(p r0, w "", w "")'
      SendMessage $0 0x2001 0 0x00392D27 ; PBM_SETBKCOLOR
      SendMessage $0 0x0409 0 0x001A7AFF ; PBM_SETBARCOLOR

      GetDlgItem $0 $cfInstallPage 1006
      !insertmacro CloudforgePosition $cfInstallPage $0 17 46 263 22
      SetCtlColors $0 "${CF_INSTALLER_MUTED}" "${CF_INSTALLER_CANVAS}"
      SendMessage $0 ${WM_SETFONT} $cfInstallerBodyFont 1
      SendMessage $0 ${WM_SETTEXT} 0 "STR:$(MUI_TEXT_INSTALLING_SUBTITLE)"

      System::Call 'user32::InvalidateRect(p $HWNDPARENT, p0, i 1)'
    FunctionEnd

    Function CloudforgeStyleFinish
      # Restore the native finishing action after a manual installation.
      ShowWindow $mui.Button.Next ${SW_SHOW}
      !insertmacro CloudforgeStyleChoice $mui.FinishPage.Run "${CF_INSTALLER_HEADER}"
    FunctionEnd

    Function .onGUIEnd
      System::Call "gdi32::DeleteObject(p $cfInstallerTitleFont)"
      System::Call "gdi32::DeleteObject(p $cfInstallerBodyFont)"
    FunctionEnd
  !macroend
!endif
