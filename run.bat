@echo off
setlocal
cd /d "%~dp0"

if exist .env (
  for /f "usebackq eol=# tokens=1,* delims==" %%A in (".env") do (
    if not defined %%A set "%%A=%%B"
  )
)

if not defined ACCESS_TOKEN set "ACCESS_TOKEN=devtoken"
if not defined DB_PATH set "DB_PATH=data\stockwatcher.db"
if not defined ADDR set "ADDR=:8080"
if not exist data mkdir data

set "CMD=%~1"
if "%CMD%"=="" set "CMD=serve"

if /I "%CMD%"=="serve" (
  echo Stockwatcher  http://127.0.0.1%ADDR%  token %ACCESS_TOKEN%
)

go run ./cmd/stockwatcher %CMD%
