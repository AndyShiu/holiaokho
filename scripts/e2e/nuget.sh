set -e
export DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_NOLOGO=1 NUGET_PACKAGES=/work/pkgs
cd /work && mkdir -p lib app && cd lib
dotnet new classlib -n HlLib -o . --force >/dev/null
cat > nuget.config <<'X'
<?xml version="1.0" encoding="utf-8"?><configuration><packageSources><clear/><add key="hl" value="http://host.docker.internal:18081/repository/nuget-group/index.json" /></packageSources></configuration>
X
dotnet add package Newtonsoft.Json --version 13.0.3 >/dev/null
echo "== restore via group"; dotnet restore 2>&1 | tail -1
echo "== pack"; dotnet pack -c Release -p:PackageId=HlLib -p:Version=1.0.0 -o out 2>&1 | grep -E "Successfully|error" | head -2
echo "== push to hosted"; dotnet nuget push out/HlLib.1.0.0.nupkg --source http://host.docker.internal:18081/repository/nuget-hosted/index.json --api-key $HL_TOKEN 2>&1 | tail -2
echo "== push again (allow_once)"; dotnet nuget push out/HlLib.1.0.0.nupkg --source http://host.docker.internal:18081/repository/nuget-hosted/index.json --api-key $HL_TOKEN 2>&1 | grep -iE "409|conflict|already|error" | head -1
cd ../app && dotnet new console -n HlApp -o . --force >/dev/null && cp ../lib/nuget.config .
echo "== add own package via group"; dotnet add package HlLib --version 1.0.0 2>&1 | tail -1
dotnet restore 2>&1 | tail -1; ls /work/pkgs/ | tr '\n' ' '; echo
echo "== search"; dotnet nuget search HlLib --source http://host.docker.internal:18081/repository/nuget-group/index.json 2>&1 | grep -i hllib | head -2
