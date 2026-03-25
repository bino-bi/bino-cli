; bino CLI Windows Installer
; Built with Inno Setup 6.x — https://jrsoftware.org/isinfo.php
;
; Build:
;   iscc /DMyAppVersion=1.0.0 installer\bino-setup.iss
;
; Requires bino.exe and duckdb.dll in dist\ (relative to repo root).

#define MyAppName "bino"
#define MyAppPublisher "Bino BI"
#define MyAppURL "https://cli.bino.bi"
#define MyAppExeName "bino.exe"

; Version injected at build time via /DMyAppVersion=x.y.z
#ifndef MyAppVersion
  #define MyAppVersion "0.0.0-dev"
#endif

[Setup]
AppId={{B1F9E3A4-D2C4-4F8B-A6E1-8C3D5F7A9B0E}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
DefaultDirName={localappdata}\bino
DisableProgramGroupPage=yes
LicenseFile=LICENCE
; SourceDir is relative to this .iss file — ".." points to the repo root.
SourceDir=..
OutputDir=dist
OutputBaseFilename=bino-cli_Windows_x86_64_setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ChangesEnvironment=yes
AllowNoIcons=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayName={#MyAppName} CLI

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "dist\bino.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "dist\duckdb.dll"; DestDir: "{app}"; Flags: ignoreversion

[Registry]
; Add install directory to user PATH (only if not already present).
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; \
  ValueData: "{olddata};{app}"; Check: NeedsAddPath(ExpandConstant('{app}'))

[Run]
; Download Chrome headless shell and DuckDB extensions after install.
Filename: "{app}\{#MyAppExeName}"; Parameters: "setup"; \
  StatusMsg: "Downloading browser runtime and extensions..."; \
  Flags: runhidden waituntilterminated

[UninstallDelete]
Type: filesandordirs; Name: "{localappdata}\bino\cache"

[Code]
// Return True when {app} is NOT yet on the user PATH.
function NeedsAddPath(Param: string): Boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKEY_CURRENT_USER,
    'Environment', 'Path', OrigPath) then
  begin
    Result := True;
    exit;
  end;
  // Wrap both strings in semicolons so partial matches are avoided.
  Result := Pos(';' + Uppercase(Param) + ';',
                ';' + Uppercase(OrigPath) + ';') = 0;
end;

// Remove {app} from user PATH on uninstall.
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Path, AppDir: string;
  P: Integer;
begin
  if CurUninstallStep = usPostUninstall then
  begin
    AppDir := ExpandConstant('{app}');
    if RegQueryStringValue(HKEY_CURRENT_USER,
      'Environment', 'Path', Path) then
    begin
      P := Pos(';' + Uppercase(AppDir), ';' + Uppercase(Path));
      if P > 0 then
      begin
        Delete(Path, P - 1, Length(AppDir) + 1);
        RegWriteStringValue(HKEY_CURRENT_USER,
          'Environment', 'Path', Path);
      end;
    end;
  end;
end;
