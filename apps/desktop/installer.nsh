!include "${PROJECT_DIR}\apps\desktop\installer-theme.nsh"

!macro customInit
  # Older Cloudforge builds pass /S together with --force-run.
  ${If} ${isUpdated}
  ${AndIf} ${isForceRun}
    SetSilent normal
  ${EndIf}
!macroend

!macro customInstallMode
  !ifndef BUILD_UNINSTALLER
    ${If} ${isUpdated}
    ${AndIf} ${isForceRun}
      ${If} $installMode == "all"
        StrCpy $isForceMachineInstall "1"
      ${Else}
        StrCpy $isForceCurrentInstall "1"
      ${EndIf}
    ${EndIf}
  !endif
!macroend

!macro customFinishPage
  Function CloudforgeSkipFinishPage
    ${If} ${isUpdated}
    ${AndIf} ${isForceRun}
      Abort
    ${EndIf}
  FunctionEnd

  Function CloudforgeRunFromFinishPage
    StrCpy $1 ""
    ${If} ${isUpdated}
      StrCpy $1 "--updated"
    ${EndIf}
    ${StdUtils.ExecShellAsUser} $0 "$launchLink" "open" "$1"
  FunctionEnd

  !define MUI_PAGE_CUSTOMFUNCTION_PRE CloudforgeSkipFinishPage
  !define MUI_PAGE_CUSTOMFUNCTION_SHOW CloudforgeStyleFinish
  !define MUI_FINISHPAGE_RUN
  !define MUI_FINISHPAGE_RUN_FUNCTION CloudforgeRunFromFinishPage
  !insertmacro MUI_PAGE_FINISH
!macroend

!macro customInstall
  ${If} ${isUpdated}
  ${AndIf} ${isForceRun}
    SetAutoClose true
  ${EndIf}
!macroend

!macro customHeader
  !ifndef BUILD_UNINSTALLER
    !insertmacro CloudforgeThemeFunctions

    Function .onInstSuccess
      ${If} ${isUpdated}
      ${AndIf} ${isForceRun}
        ${If} ${UAC_IsAdmin}
          ${StdUtils.ExecShellAsUser} $0 "$appExe" "open" "--updated"
          ${If} $0 == "ok"
          ${OrIf} $0 == "fallback"
            Return
          ${EndIf}
        ${Else}
          ClearErrors
          Exec '"$appExe" --updated'
          ${IfNot} ${Errors}
            Return
          ${EndIf}
        ${EndIf}
        ${If} $LANGUAGE == ${LANG_PORTUGUESEBR}
          MessageBox MB_OK|MB_ICONEXCLAMATION "A atualização foi instalada. Para concluir, abra o Cloudforge pelo menu Iniciar.$\r$\n$\r$\n$appExe"
        ${Else}
          MessageBox MB_OK|MB_ICONEXCLAMATION "The update was installed. To finish, open Cloudforge from the Start menu.$\r$\n$\r$\n$appExe"
        ${EndIf}
      ${EndIf}
    FunctionEnd
  !endif
!macroend
