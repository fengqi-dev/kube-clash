package helper

import (
	"strings"
	"testing"
)

func TestElevatedPowerShellLauncherUsesRunAsParameterSet(t *testing.T) {
	launcher := elevatedPowerShellLauncher("encoded")
	for _, forbidden := range []string{
		"RedirectStandardOutput",
		"RedirectStandardError",
	} {
		if strings.Contains(launcher, forbidden) {
			t.Fatalf("launcher contains incompatible Start-Process option %s", forbidden)
		}
	}
	for _, required := range []string{"-Verb RunAs", "-Wait", "-PassThru"} {
		if !strings.Contains(launcher, required) {
			t.Fatalf("launcher does not contain %s", required)
		}
	}
}

func TestElevatedPowerShellPayloadCapturesChildOutput(t *testing.T) {
	payload := elevatedPowerShellPayload("Write-Output 'ok'", `C:\Temp\a'b.out`)
	if !strings.Contains(payload, "Out-File") {
		t.Fatal("payload does not capture elevated output")
	}
	if !strings.Contains(payload, `'C:\Temp\a''b.out'`) {
		t.Fatal("payload does not safely quote the output path")
	}
}
