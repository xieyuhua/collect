@echo off
setlocal
cd /d "%~dp0"
if not exist "bin\collect.exe" (
  echo [collect] 未找到 bin\collect.exe，开始编译...
  where go >nul 2>nul
  if errorlevel 1 (
    echo [collect] 未安装 Go，请先安装 https://go.dev/dl/ 或使用已编译的 bin\collect.exe
    pause
    exit /b 1
  )
  if not exist bin mkdir bin
  go build -o bin\collect.exe .
  if errorlevel 1 (
    echo [collect] 编译失败
    pause
    exit /b 1
  )
)
echo [collect] 启动中... 关闭本窗口即停止服务
bin\collect.exe %*
pause