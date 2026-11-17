# 构建内置插件包：按 manifest 的 runtime.commands 交叉编译 native 插件，
# 再把 manifest、README 与各平台产物打包到 internal/builtin/plugins。
# 用法：powershell -File scripts/build-builtin-plugins.ps1
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$plugins = @(
    @{ Dir = 'cmd/plugin-filesystem'; Name = 'file-system' }
)

foreach ($plugin in $plugins) {
    $source = Join-Path $root $plugin.Dir
    $manifestPath = Join-Path $source 'manifest.json'
    $manifest = Get-Content $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json

    if ($manifest.runtime.type -ne 'native') {
        throw "插件 $($manifest.id) 不是 native 运行方式，脚本暂不支持"
    }

    $stage = Join-Path $root "build/$($manifest.id)"
    if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
    New-Item -ItemType Directory -Path $stage -Force | Out-Null

    Copy-Item $manifestPath (Join-Path $stage 'manifest.json')
    Copy-Item (Join-Path $source 'README.md') (Join-Path $stage 'README.md')

    foreach ($platform in $manifest.runtime.commands.PSObject.Properties) {
        $parts = $platform.Name.Split('-')
        $env:GOOS = $parts[0]
        $env:GOARCH = $parts[1]
        $target = Join-Path $stage ($platform.Value -replace '/', '\')
        New-Item -ItemType Directory -Path (Split-Path $target) -Force | Out-Null
        Write-Host "编译 $($manifest.id) $($platform.Name)"
        go build -trimpath -ldflags '-s -w' -o $target "./$($plugin.Dir)"
        if ($LASTEXITCODE -ne 0) { throw "编译 $($platform.Name) 失败" }
    }
    Remove-Item Env:GOOS
    Remove-Item Env:GOARCH

    # 同 ID 的旧版本包会被移除，避免启动时重复安装。
    Get-ChildItem (Join-Path $root 'internal/builtin/plugins') -Filter "$($manifest.id)-*.tar" |
        Remove-Item -Force

    $archive = Join-Path $root "internal/builtin/plugins/$($manifest.id)-$($manifest.version).tar"
    Push-Location $stage
    try {
        tar -cf $archive manifest.json README.md bin
        if ($LASTEXITCODE -ne 0) { throw '打包 tar 失败' }
    }
    finally {
        Pop-Location
    }
    Write-Host "已生成 $archive"
}
