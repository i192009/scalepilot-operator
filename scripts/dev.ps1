param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Command)
$ErrorActionPreference = 'Stop'
$projectPath = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$containerName = 'scalepilot-dev'
$exists = docker ps -a --filter "name=^/$containerName`$" --format '{{.Names}}'
if (-not $exists) {
    docker run -d --init --name $containerName -v "${projectPath}:/workspace" -v /var/run/docker.sock:/var/run/docker.sock -v scalepilot-gomod:/go/pkg/mod -v scalepilot-gocache:/root/.cache/go-build -w /workspace golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea sleep infinity | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not create development container' }
}
docker start $containerName | Out-Null
if (-not $Command -or $Command[0] -eq 'setup') {
    docker exec $containerName bash scripts/bootstrap-tools.sh
} elseif ($Command[0] -eq 'connect-kind') {
    $networks = docker inspect --format '{{json .NetworkSettings.Networks}}' $containerName | ConvertFrom-Json
    if (-not ($networks.PSObject.Properties.Name -contains 'kind')) {
        docker network connect kind $containerName
    }
    docker exec $containerName bash -c 'mkdir -p /root/.kube && kind get kubeconfig --name scalepilot --internal > /root/.kube/config'
} elseif ($Command[0] -eq 'export-kubeconfig') {
    $kubePath = Join-Path $projectPath '.kube'
    [System.IO.Directory]::CreateDirectory($kubePath) | Out-Null
    $configText = docker exec $containerName kind get kubeconfig --name scalepilot
    if ($LASTEXITCODE -ne 0) { throw 'Could not export Kind credentials' }
    [System.IO.File]::WriteAllText((Join-Path $kubePath 'config'), ($configText -join "`n"), [System.Text.UTF8Encoding]::new($false))
    Write-Output 'Kubeconfig written to .kube/config (gitignored).'
} else {
    docker exec $containerName @Command
}
if ($LASTEXITCODE -ne 0) { throw "Development command failed with exit code $LASTEXITCODE" }
