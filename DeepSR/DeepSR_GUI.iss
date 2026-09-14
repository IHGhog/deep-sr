#define MyAppName "DeepSR"
#define MyAppVersion "3.1.0"
#define MyAppPublisher "DeepSR Team"
#define MyAppExeName "deepsr.exe"
#ifndef IncludeModels
  #define IncludeModels "none"
#endif

#if IncludeModels == "full"
  #define MyOutputBaseFilename "DeepSR-Setup-v" + MyAppVersion + "-Full"
#elif IncludeModels == "fp16"
  #define MyOutputBaseFilename "DeepSR-Setup-v" + MyAppVersion + "-FP16"
#elif IncludeModels == "fp32"
  #define MyOutputBaseFilename "DeepSR-Setup-v" + MyAppVersion + "-FP32"
#else
  #define MyOutputBaseFilename "DeepSR-Setup-v" + MyAppVersion
#endif

[Setup]
AppId={{C8A53B22-832F-4F92-A721-3F719E0286A1}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
ArchitecturesInstallIn64BitMode=x64compatible
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
AllowNoIcons=yes
OutputDir=..\release
OutputBaseFilename={#MyOutputBaseFilename}
SetupIconFile=..\DeepSR\build\windows\icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
Compression=lzma2/ultra64
InternalCompressLevel=ultra64
SolidCompression=yes
LZMANumBlockThreads=8
WizardStyle=modern
PrivilegesRequired=admin
DisableWelcomePage=yes
DisableDirPage=no
DisableProgramGroupPage=yes

[Languages]
Name: "chinesesimplified"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[Dirs]
Name: "{app}\bin\models"

[Files]
; GUI 主程序
Source: "..\DeepSR\build\bin\deepsr.exe"; DestDir: "{app}"; Flags: ignoreversion
; 核心 CLI 引擎、FFmpeg 工具链与运行时 DLL
Source: "..\DeepSR\build\bin\bin\*.exe"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "..\DeepSR\build\bin\bin\*.dll"; DestDir: "{app}\bin"; Flags: ignoreversion
#if IncludeModels == "full"
; 集成 FP32 + FP16 全部预置模型
Source: "..\DeepSRCli\bin\models\*"; DestDir: "{app}\bin\models"; Flags: ignoreversion
#elif IncludeModels == "fp16"
; 仅集成 FP16 预置模型
Source: "..\DeepSRCli\bin\models\*fp16*"; DestDir: "{app}\bin\models"; Flags: ignoreversion
#elif IncludeModels == "fp32"
; 仅集成 FP32 预置模型
Source: "..\DeepSRCli\bin\models\*fp32*"; DestDir: "{app}\bin\models"; Flags: ignoreversion
#endif

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent

[Code]
var
  ShouldDeleteConfig: Boolean;

// 安装完成后将注册表中的卸载命令行修正为静默模式，防止系统二次弹出默认提示
procedure CurStepChanged(CurStep: TSetupStep);
var
  AppKey: String;
  UninsStr: String;
begin
  if CurStep = ssPostInstall then begin
    AppKey := 'Software\Microsoft\Windows\CurrentVersion\Uninstall\' + ExpandConstant('{#SetupSetting("AppId")}') + '_is1';
    UninsStr := ExpandConstant('"{uninstallexe}" /SILENT');
    RegWriteStringValue(HKLM64, AppKey, 'UninstallString', UninsStr);
    RegWriteStringValue(HKLM32, AppKey, 'UninstallString', UninsStr);
  end;
end;

// 卸载前原生确认窗体与进程关闭检查 (单窗体体验，无二次弹窗)
function InitializeUninstall(): Boolean;
var
  Form: TSetupForm;
  PromptLabel: TLabel;
  PathLabel: TLabel;
  ConfigCheckBox: TNewCheckBox;
  ConfigPathLabel: TLabel;
  OkBtn, CancelBtn: TNewButton;
  ResultCode: Integer;
begin
  // 若用户直接双击 unins000.exe (非 /SILENT 启动)，自动转为 /SILENT 启动以屏蔽 InnoSetup 内置确认弹窗
  if not UninstallSilent then begin
    Exec(ExpandConstant('{uninstallexe}'), '/SILENT', '', SW_SHOW, ewNoWait, ResultCode);
    Result := False;
    Exit;
  end;

  // 1. 卸载前先强制关闭可能在运行的 deepsr.exe 与 deepsr-cli.exe，确保 WebView2 与 DLL 不被锁死
  Exec('taskkill.exe', '/F /IM deepsr.exe /IM deepsr-cli.exe /T', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(300);

  // 2. 原生确认窗体
  Form := CreateCustomForm(ScaleX(440), ScaleY(215), False, True);
  try
    Form.Caption := 'DeepSR 卸载';
    Form.Position := poScreenCenter;
    Form.BorderStyle := bsDialog;

    PromptLabel := TLabel.Create(Form);
    PromptLabel.Parent := Form;
    PromptLabel.Left := ScaleX(24);
    PromptLabel.Top := ScaleY(20);
    PromptLabel.Width := ScaleX(392);
    PromptLabel.Caption := '您确定要从计算机中完全移除 DeepSR 吗？';
    PromptLabel.Font.Style := [fsBold];
    PromptLabel.Font.Size := 10;

    PathLabel := TLabel.Create(Form);
    PathLabel.Parent := Form;
    PathLabel.Left := ScaleX(24);
    PathLabel.Top := ScaleY(48);
    PathLabel.Width := ScaleX(392);
    PathLabel.Caption := '安装路径: ' + ExpandConstant('{app}');

    // 复选框：删除配置文件和缓存数据
    ConfigCheckBox := TNewCheckBox.Create(Form);
    ConfigCheckBox.Parent := Form;
    ConfigCheckBox.Left := ScaleX(24);
    ConfigCheckBox.Top := ScaleY(85);
    ConfigCheckBox.Width := ScaleX(392);
    ConfigCheckBox.Caption := '删除配置文件和缓存数据';
    ConfigCheckBox.Checked := True; // 默认勾选

    // 复选框下方的全路径显示 (小字半透明/灰色)
    ConfigPathLabel := TLabel.Create(Form);
    ConfigPathLabel.Parent := Form;
    ConfigPathLabel.Left := ScaleX(42);
    ConfigPathLabel.Top := ScaleY(108);
    ConfigPathLabel.Width := ScaleX(374);
    ConfigPathLabel.Caption := ExpandConstant('{userappdata}\DeepSR');
    ConfigPathLabel.Font.Size := 8;
    ConfigPathLabel.Font.Color := clGrayText;

    OkBtn := TNewButton.Create(Form);
    OkBtn.Parent := Form;
    OkBtn.Left := ScaleX(235);
    OkBtn.Top := ScaleY(158);
    OkBtn.Width := ScaleX(88);
    OkBtn.Height := ScaleY(28);
    OkBtn.Caption := '确认卸载';
    OkBtn.ModalResult := mrOk;
    OkBtn.Default := True;

    CancelBtn := TNewButton.Create(Form);
    CancelBtn.Parent := Form;
    CancelBtn.Left := ScaleX(332);
    CancelBtn.Top := ScaleY(158);
    CancelBtn.Width := ScaleX(88);
    CancelBtn.Height := ScaleY(28);
    CancelBtn.Caption := '取消';
    CancelBtn.ModalResult := mrCancel;
    CancelBtn.Cancel := True;

    if Form.ShowModal() = mrOk then begin
      ShouldDeleteConfig := ConfigCheckBox.Checked;
      Result := True;
    end else begin
      Result := False;
    end;
  finally
    Form.Free();
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  AppDataDir: String;
  ResultCode: Integer;
begin
  if CurUninstallStep = usUninstall then begin
    // 再次确保进程已结束
    Exec('taskkill.exe', '/F /IM deepsr.exe /IM deepsr-cli.exe /T', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    Sleep(200);
  end;
  if CurUninstallStep = usPostUninstall then begin
    if ShouldDeleteConfig then begin
      AppDataDir := ExpandConstant('{userappdata}\DeepSR');
      if DirExists(AppDataDir) then begin
        DelTree(AppDataDir, True, True, True);
      end;
    end;
  end;
end;
