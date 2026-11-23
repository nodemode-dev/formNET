@echo off
setlocal EnableExtensions DisableDelayedExpansion
cd /d "%~dp0"

set "BUNDLES=msi,nsis"
set "CONFIG="
set "SERVER_ONLY=0"
set "ALL=0"
set "SKIP_WEB=0"
set "DEBUG=0"
set "CLEAN=0"
set "YES=0"
:args
if "%~1"=="" goto checked_args
if /i "%~1"=="--server-only" set "SERVER_ONLY=1" & shift & goto args
if /i "%~1"=="--all" set "ALL=1" & shift & goto args
if /i "%~1"=="--skip-web" set "SKIP_WEB=1" & shift & goto args
if /i "%~1"=="--debug" set "DEBUG=1" & shift & goto args
if /i "%~1"=="--clean" set "CLEAN=1" & shift & goto args
if /i "%~1"=="--yes" set "YES=1" & shift & goto args
if /i "%~1"=="-y" set "YES=1" & shift & goto args
if /i "%~1"=="--bundles" goto arg_bundles
if /i "%~1"=="--config" goto arg_config
if /i "%~1"=="--help" goto usage
if /i "%~1"=="-h" goto usage
echo [build_app] unknown option: %~1
goto usage_fail

:arg_bundles
set "BUNDLES=%~2"
if not defined BUNDLES (echo [build_app] --bundles needs a list & exit /b 2)
if "%BUNDLES:~0,1%"=="-" (echo [build_app] --bundles needs a list & exit /b 2)
shift & shift
:arg_bundles_more
set "NEXT=%~1"
if not defined NEXT goto args
if "%NEXT:~0,1%"=="-" goto args
set "BUNDLES=%BUNDLES%,%NEXT%"
shift
goto arg_bundles_more

:arg_config
set "CONFIG=%2"
if not defined CONFIG (echo [build_app] --config needs JSON or a JSON file & exit /b 2)
shift & shift
goto args

:checked_args
where go >nul 2>nul || (echo [build_app] Go 1.26 or newer is required: https://go.dev/dl/ & exit /b 1)
where node >nul 2>nul || (echo [build_app] Node 22 or newer is required: https://nodejs.org/ & exit /b 1)
if not defined GOTOOLCHAIN set "GOTOOLCHAIN=auto"
set "NODE_V="
set "NODE_MAJOR=0"
for /f "delims=" %%v in ('node -v') do set "NODE_V=%%v"
for /f "tokens=1 delims=v." %%m in ("%NODE_V%") do set "NODE_MAJOR=%%m"
if %NODE_MAJOR% LSS 22 (echo [build_app] Node 22 or newer is required, found %NODE_V% & exit /b 1)

set "APPV="
for /f "delims=" %%v in ('node -p "require('./web/package.json').version"') do set "APPV=%%v"
echo %APPV%| findstr /r /b "[0-9][0-9]*\.[0-9][0-9]*\.[0-9]" >nul || (echo [build_app] web/package.json carries no x.y.z version & exit /b 1)
set "VERSION=v%APPV%"
set "MAKE_V="
for /f "tokens=3" %%v in ('findstr /r /b /c:"VERSION  *:=" Makefile') do if not defined MAKE_V set "MAKE_V=%%v"
if not defined MAKE_V (echo [build_app] the Makefile carries no VERSION := vX.Y.Z line & exit /b 1)
if not "%MAKE_V%"=="%VERSION%" (echo [build_app] the two version sources differ: the Makefile has VERSION := %MAKE_V% and web/package.json has "version": "%APPV%"; set both to the same version & exit /b 1)
echo [build_app] FormNET %VERSION%

if "%SKIP_WEB%"=="1" goto web_ready
if exist web\node_modules goto web_build
echo [build_app] installing the web client's packages
pushd web
call npm ci --no-audit --no-fund || (popd & exit /b 1)
popd
:web_build
echo [build_app] building the web client
pushd web
call npm run build || (popd & exit /b 1)
popd
:web_ready
if not exist web\dist\index.html (echo [build_app] web\dist\index.html is missing: run without --skip-web & exit /b 1)

set "CGO_ENABLED=0"
set "LDFLAGS=-s -w -X github.com/nodemode-dev/formnet/internal/app.Version=%VERSION%"
if "%ALL%"=="1" goto release
if not exist bin mkdir bin
set "GO_OS="
set "GO_ARCH="
for /f "delims=" %%v in ('go env GOOS') do set "GO_OS=%%v"
for /f "delims=" %%v in ('go env GOARCH') do set "GO_ARCH=%%v"
echo [build_app] compiling bin\formnet.exe for %GO_OS%/%GO_ARCH%
go build -trimpath -ldflags "%LDFLAGS%" -o bin\formnet.exe ./cmd/formnet || exit /b 1
for %%f in (bin\formnet.exe) do echo [build_app] done: bin\formnet.exe, %%~zf bytes
if "%SERVER_ONLY%"=="1" exit /b 0

echo [build_app] checking the desktop toolchain
if exist "%USERPROFILE%\.cargo\bin\rustc.exe" set "PATH=%USERPROFILE%\.cargo\bin;%PATH%"
set "HAVE_CARGO=0"
where cargo >nul 2>nul && set "HAVE_CARGO=1"
where rustup >nul 2>nul && if "%HAVE_CARGO%"=="1" goto rust_found
if "%HAVE_CARGO%"=="1" echo [build_app] the Rust on PATH is not managed by rustup; the desktop build expects rustup
if "%YES%"=="1" goto rust_install
if "%HAVE_CARGO%"=="1" goto rust_found
echo [build_app] stopped: Rust is needed for the desktop app. Install it from https://rustup.rs, or run build_app.bat --yes to install it, and run build_app.bat again
exit /b 1
:rust_install
set "RUSTUP_ARCH=x86_64"
if /i "%PROCESSOR_ARCHITECTURE%"=="ARM64" set "RUSTUP_ARCH=aarch64"
where curl >nul 2>nul || (echo [build_app] curl is needed to fetch rustup; install Rust from https://rustup.rs & exit /b 1)
echo [build_app] installing Rust with rustup from https://rustup.rs into %USERPROFILE%\.cargo
set "RUSTUP_INIT=%TEMP%\formnet-rustup-init.exe"
curl --proto =https --tlsv1.2 -sSfL -o "%RUSTUP_INIT%" https://win.rustup.rs/%RUSTUP_ARCH% || (echo [build_app] could not download rustup & exit /b 1)
"%RUSTUP_INIT%" -y --profile minimal --default-toolchain stable || (echo [build_app] the Rust install failed & exit /b 1)
del /q "%RUSTUP_INIT%" 2>nul
set "PATH=%USERPROFILE%\.cargo\bin;%PATH%"
:rust_found
where rustup >nul 2>nul && (rustup show active-toolchain >nul 2>nul || rustup default stable)
for %%v in (RUSTFLAGS CARGO_ENCODED_RUSTFLAGS CARGO_BUILD_TARGET CARGO_TARGET_DIR RUSTC RUSTC_WRAPPER) do set "%%v="
set "RUSTC_V="
set "CARGO_V="
for /f "tokens=2" %%v in ('rustc -V') do set "RUSTC_V=%%v"
for /f "tokens=2" %%v in ('cargo -V') do set "CARGO_V=%%v"
if not defined RUSTC_V (echo [build_app] rustc does not run; install Rust from https://rustup.rs & exit /b 1)
if not "%RUSTC_V%"=="%CARGO_V%" (echo [build_app] rustc %RUSTC_V% and cargo %CARGO_V% come from different installs. Remove the extra one and keep rustup's & exit /b 1)
set "TRIPLE="
for /f "tokens=2" %%t in ('rustc -vV ^| findstr /b host:') do set "TRIPLE=%%t"
if not defined TRIPLE (echo [build_app] rustc -vV names no host & exit /b 1)
echo %TRIPLE%| findstr /c:"-pc-windows-" >nul || (echo [build_app] Rust builds for %TRIPLE%, not Windows. Run: rustup default stable-msvc & exit /b 1)
echo [build_app] Rust %RUSTC_V% for %TRIPLE%

if exist web\node_modules\.bin\tauri.cmd goto tauri_found
pushd web
call npm ci --no-audit --no-fund || (popd & exit /b 1)
popd
:tauri_found
if not exist web\node_modules\.bin\tauri.cmd (echo [build_app] the Tauri command line is missing from web\node_modules; run npm install in the web folder & exit /b 1)
set "CLI_V="
set "CRATE_V="
set "CLI_MM=cli"
set "CRATE_MM=crate"
for /f "tokens=2" %%v in ('call web\node_modules\.bin\tauri.cmd --version') do set "CLI_V=%%v"
for /f "delims=" %%v in ('node -p "/name = .tauri.\r?\nversion = .([0-9.]+)./.exec(require('fs').readFileSync('src-tauri/Cargo.lock','utf8'))?.[1]"') do set "CRATE_V=%%v"
for /f "tokens=1,2 delims=." %%a in ("%CLI_V%") do set "CLI_MM=%%a.%%b"
for /f "tokens=1,2 delims=." %%a in ("%CRATE_V%") do set "CRATE_MM=%%a.%%b"
if not "%CLI_MM%"=="%CRATE_MM%" (echo [build_app] the Tauri command line %CLI_V% and the tauri crate %CRATE_V% differ; update web/package.json or src-tauri/Cargo.lock so they match & exit /b 1)

findstr /l /c:"\"version\": \"%APPV%\"" src-tauri\tauri.conf.json >nul || (echo [build_app] src-tauri\tauri.conf.json does not carry version %APPV%; bump it with the release & exit /b 1)
findstr /b /l /c:"version = \"%APPV%\"" src-tauri\Cargo.toml >nul || (echo [build_app] src-tauri\Cargo.toml does not carry version %APPV%; bump it with the release & exit /b 1)

if not "%CLEAN%"=="1" goto cleaned
echo [build_app] clearing the Rust build cache
pushd src-tauri
cargo clean || (popd & exit /b 1)
popd
:cleaned
if not exist src-tauri\binaries mkdir src-tauri\binaries
del /q src-tauri\binaries\formnet-server-* 2>nul
set "SIDECAR=src-tauri\binaries\formnet-server-%TRIPLE%.exe"
copy /y bin\formnet.exe "%SIDECAR%" >nul || exit /b 1
"%SIDECAR%" version >nul 2>nul || (echo [build_app] the server copy at %SIDECAR% does not run on this computer & exit /b 1)
set "SERVER_V="
for /f "delims=" %%v in ('call "%SIDECAR%" version') do set "SERVER_V=%%v"
echo [build_app] server inside the app: %SIDECAR%, %SERVER_V%

set "PROFILE=release"
set "TAURI_EXTRA="
if "%DEBUG%"=="1" set "PROFILE=debug"
if "%DEBUG%"=="1" set "TAURI_EXTRA=--debug"
echo [build_app] bundling %BUNDLES%
pushd src-tauri
if defined CONFIG goto bundle_config
call ..\web\node_modules\.bin\tauri.cmd build --bundles %BUNDLES% %TAURI_EXTRA%
goto bundle_done
:bundle_config
call ..\web\node_modules\.bin\tauri.cmd build --bundles %BUNDLES% %TAURI_EXTRA% --config %CONFIG%
:bundle_done
set "STATUS=%ERRORLEVEL%"
popd
if not "%STATUS%"=="0" (echo [build_app] the desktop build failed; the log above shows the first error & exit /b %STATUS%)
set "OUT=src-tauri\target\%PROFILE%\bundle"
echo [build_app] done. The installers are in %OUT%
for %%f in ("%OUT%\msi\*.msi" "%OUT%\nsis\*.exe") do echo [build_app]   %%~ff
echo [build_app] your recordings stay in memory; the root certificate lives in %USERPROFILE%\formnet (set FORMNET_DATA before starting the app to use another folder)
exit /b 0

:release
if exist release rmdir /s /q release
mkdir release
call :cross linux amd64 || exit /b 1
call :cross linux arm64 || exit /b 1
call :cross darwin amd64 || exit /b 1
call :cross darwin arm64 || exit /b 1
call :cross windows amd64 .exe || exit /b 1
type nul > release\SHA256SUMS
for %%f in (release\formnet_*) do call :sum "%%f" || exit /b 1
echo [build_app] done: release\ with SHA256SUMS
exit /b 0

:cross
setlocal
set "GOOS=%~1"
set "GOARCH=%~2"
set "OUTFILE=release\formnet_%VERSION%_%~1_%~2%~3"
echo [build_app] compiling %OUTFILE%
go build -trimpath -ldflags "%LDFLAGS%" -o "%OUTFILE%" ./cmd/formnet || exit /b 1
endlocal
exit /b 0

:sum
set "HASH="
for /f "delims=" %%h in ('certutil -hashfile "%~1" SHA256 ^| findstr /v /c:":"') do if not defined HASH set "HASH=%%h"
if not defined HASH (echo [build_app] certutil could not hash %~1 & exit /b 1)
set "HASH=%HASH: =%"
>>release\SHA256SUMS echo %HASH%  %~nx1
exit /b 0

:usage
call :usage_text
exit /b 0
:usage_fail
echo.
call :usage_text
exit /b 2
:usage_text
echo usage: build_app.bat [--server-only] [--all] [--skip-web] [--debug] [--bundles LIST] [--clean] [--yes] [--config JSON]
echo.
echo Builds FormNET start to finish on Windows: the desktop app with the server inside it.
echo   1. checks Go 1.26+ and Node 22+, and that the Makefile and web\package.json carry the same version
echo   2. installs the web client's packages when they are missing
echo   3. builds the web client into web\dist, embedded into the binary
echo   4. compiles bin\formnet.exe
echo   5. checks the desktop toolchain: Rust through rustup, the Tauri command line against the tauri crate
echo   6. bundles the desktop app installers, msi and nsis, in src-tauri\target\release\bundle
echo.
echo Options:
echo   --server-only    stop after step 4, the server binary alone
echo   --all            every server release target into release\ with SHA256SUMS, no desktop app
echo   --skip-web       reuse the web client already in web\dist
echo   --debug          a debug build of the desktop app
echo   --bundles LIST   which installers, for example msi or msi,nsis
echo   --clean          clear the Rust build cache and the old server copy first
echo   --yes            install Rust with rustup when it is missing
echo   --config JSON    extra Tauri settings: a JSON file, or JSON as one quoted argument
echo.
echo On macOS and Linux run ./build_app. Run the server alone with bin\formnet.exe.
exit /b 0
