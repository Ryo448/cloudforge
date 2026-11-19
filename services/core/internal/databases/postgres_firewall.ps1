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

if ($Apply -and $Ports -eq '-') {
    if ($null -ne $rule) { $rules.Remove($Name) }
    return
}

# NET_FW_MODIFY_STATE_OK. Policy can ignore local rules or block all inbound traffic.
if ($policy.LocalPolicyModifyState -ne 0) {
    if ($Apply) { exit 11 }
    'policy'
    return
}

$allProfiles = 2147483647
$valid = $null -ne $rule -and $rule.Enabled -and $rule.ApplicationName -eq $Program -and
    $rule.Direction -eq 1 -and $rule.Action -eq 1 -and $rule.Protocol -eq 6 -and
    $rule.LocalPorts -ne '*' -and $rule.LocalAddresses -eq '*' -and
    $rule.RemotePorts -eq '*' -and $rule.RemoteAddresses -eq 'LocalSubnet' -and
    $rule.Profiles -eq $allProfiles -and !$rule.EdgeTraversal -and $rule.InterfaceTypes -eq 'All'
if (!$Apply) {
    if ($null -eq $rule) { 'absent' }
    elseif ($valid) { $rule.LocalPorts }
    else { 'invalid' }
    return
}
if ($valid -and $rule.LocalPorts -eq $Ports) { return }

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
