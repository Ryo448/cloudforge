param(
    [string] $Name,
    [string] $Ports,
    [string] $Program,
    [switch] $Apply
)

$ErrorActionPreference = 'Stop'
$policy = New-Object -ComObject HNetCfg.FwPolicy2
$rules = $policy.Rules
$rule = $null
foreach ($entry in $rules) {
    if ($entry.Name -eq $Name) {
        $rule = $entry
        break
    }
}

if ($Ports -eq '') {
    if ($null -eq $rule) { exit 0 }
    if (!$Apply) { exit 10 }
    $rules.Remove($Name)
    exit 0
}

# NET_FW_MODIFY_STATE_OK. Policy can ignore local rules or block all inbound traffic.
if ($policy.LocalPolicyModifyState -ne 0) { exit 11 }

$allProfiles = 2147483647
if ($null -ne $rule -and $rule.Enabled -and $rule.ApplicationName -eq $Program -and
    $rule.Direction -eq 1 -and $rule.Action -eq 1 -and $rule.Protocol -eq 6 -and
    $rule.LocalPorts -eq $Ports -and $rule.LocalAddresses -eq '*' -and
    $rule.RemotePorts -eq '*' -and $rule.RemoteAddresses -eq 'LocalSubnet' -and
    $rule.Profiles -eq $allProfiles -and !$rule.EdgeTraversal -and $rule.InterfaceTypes -eq 'All') {
    exit 0
}
if (!$Apply) { exit 10 }

$isNew = $null -eq $rule
if ($isNew) {
    $rule = New-Object -ComObject HNetCfg.FWRule
    $rule.Name = $Name
} else {
    $rule.Enabled = $false
}
$rule.ApplicationName = $Program
$rule.Direction = 1
$rule.Action = 1
$rule.Protocol = 6
$rule.LocalPorts = $Ports
$rule.LocalAddresses = '*'
$rule.RemotePorts = '*'
$rule.RemoteAddresses = 'LocalSubnet'
$rule.Profiles = $allProfiles
$rule.InterfaceTypes = 'All'
$rule.EdgeTraversal = $false
$rule.Enabled = $true
if ($isNew) { $rules.Add($rule) }
