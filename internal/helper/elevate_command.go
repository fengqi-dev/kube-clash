package helper

import (
	"fmt"
	"strings"
)

// elevatedPowerShellPayload records output from inside the elevated process.
// Start-Process cannot combine -Verb RunAs with its RedirectStandard* options.
func elevatedPowerShellPayload(script, outputPath string) string {
	return fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
try {
    & {
%s
    } *>&1 | Out-File -LiteralPath %s -Encoding utf8
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} catch {
    ($_ | Out-String) | Out-File -LiteralPath %s -Encoding utf8 -Append
    exit 1
}
`, script, quotePowerShellLiteral(outputPath), quotePowerShellLiteral(outputPath))
}

func elevatedPowerShellLauncher(encoded string) string {
	return fmt.Sprintf(
		`$p = Start-Process -FilePath 'powershell.exe' `+
			`-ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-EncodedCommand','%s') `+
			`-Verb RunAs -Wait -PassThru; exit $p.ExitCode`,
		encoded,
	)
}

func quotePowerShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
